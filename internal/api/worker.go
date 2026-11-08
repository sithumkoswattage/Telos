package api

// Worker API (TEL-80): the endpoints a worker program calls.
//
//	POST /api/workers/register            {"name"}                 -> {"id"}
//	POST /api/worker/tasks/claim          {"worker_id"}            -> task, or 204 if none
//	POST /api/worker/tasks/{id}/start     {"worker_id"}
//	POST /api/worker/tasks/{id}/complete  {"worker_id", "result"}
//	POST /api/worker/tasks/{id}/fail      {"worker_id", "error"}
//
// TODO: workers are not authenticated yet; tokens come with TEL-28.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"telos/internal/queue"
)

// WorkerHandler serves the worker API.
type WorkerHandler struct {
	DB    *sql.DB
	Queue *queue.Queue
}

type workerRequest struct {
	WorkerID string          `json:"worker_id"`
	Name     string          `json:"name"`
	Result   json.RawMessage `json:"result"`
	Error    string          `json:"error"`
}

// TaskResponse is what a worker receives when it claims a task.
type TaskResponse struct {
	ID       string          `json:"id"`
	JobID    string          `json:"job_id"`
	Attempts int             `json:"attempts"`
	Input    json.RawMessage `json:"input"`
}

// readWorkerRequest decodes the body and, if needWorker is true, checks that
// worker_id is a valid UUID. It writes the error reply itself.
func readWorkerRequest(w http.ResponseWriter, r *http.Request, needWorker bool) (workerRequest, bool) {
	var req workerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return req, false
	}
	if needWorker {
		if _, err := uuid.Parse(req.WorkerID); err != nil {
			writeError(w, http.StatusBadRequest, "worker_id must be a valid UUID")
			return req, false
		}
	}
	return req, true
}

// writeQueueError turns queue errors into HTTP replies.
func writeQueueError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, queue.ErrNotFound):
		writeError(w, http.StatusNotFound, "task not found")
	case errors.Is(err, queue.ErrConflict):
		writeError(w, http.StatusConflict, "this worker does not hold the task, or it is already finished")
	default:
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// RegisterWorker handles POST /api/workers/register.
func (h *WorkerHandler) RegisterWorker(w http.ResponseWriter, r *http.Request) {
	req, ok := readWorkerRequest(w, r, false)
	if !ok {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	// Same name again (e.g. the worker restarted) -> same ID, marked online.
	var id string
	err := h.DB.QueryRowContext(r.Context(), `
		INSERT INTO workers (name, token_hash, status, last_heartbeat)
		VALUES ($1, '', 'online', now())
		ON CONFLICT (name) DO UPDATE SET status = 'online', last_heartbeat = now()
		RETURNING id::text`, name).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

// Claim handles POST /api/worker/tasks/claim.
func (h *WorkerHandler) Claim(w http.ResponseWriter, r *http.Request) {
	req, ok := readWorkerRequest(w, r, true)
	if !ok {
		return
	}
	task, err := h.Queue.ClaimTask(r.Context(), req.WorkerID)
	if errors.Is(err, queue.ErrEmpty) {
		w.WriteHeader(http.StatusNoContent) // nothing to do right now
		return
	}
	if err != nil {
		writeQueueError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, TaskResponse{
		ID: task.ID, JobID: task.JobID, Attempts: task.Attempts, Input: task.Input,
	})
}

// Start handles POST /api/worker/tasks/{id}/start.
func (h *WorkerHandler) Start(w http.ResponseWriter, r *http.Request) {
	req, ok := readWorkerRequest(w, r, true)
	if !ok {
		return
	}
	if err := h.Queue.StartTask(r.Context(), r.PathValue("id"), req.WorkerID); err != nil {
		writeQueueError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Complete handles POST /api/worker/tasks/{id}/complete.
func (h *WorkerHandler) Complete(w http.ResponseWriter, r *http.Request) {
	req, ok := readWorkerRequest(w, r, true)
	if !ok {
		return
	}
	if len(req.Result) == 0 || !json.Valid(req.Result) {
		writeError(w, http.StatusBadRequest, "result must be JSON")
		return
	}
	if err := h.Queue.CompleteTask(r.Context(), r.PathValue("id"), req.WorkerID, req.Result); err != nil {
		writeQueueError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Fail handles POST /api/worker/tasks/{id}/fail.
func (h *WorkerHandler) Fail(w http.ResponseWriter, r *http.Request) {
	req, ok := readWorkerRequest(w, r, true)
	if !ok {
		return
	}
	retried, err := h.Queue.FailTask(r.Context(), r.PathValue("id"), req.WorkerID, req.Error)
	if err != nil {
		writeQueueError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"retried": retried})
}
