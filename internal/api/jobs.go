package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"telos/internal/job"
	"telos/internal/model"
)

// maxSubmitBytes caps the request body (text typed in the form or the
// contents of an uploaded .txt file).
const maxSubmitBytes = 5 << 20 // 5 MiB

type JobsHandler struct {
	DB *sql.DB
}

// writeError sends {"error": msg} with the given status.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// Submit handles POST /api/jobs.
//
// Request:  {"user_id":"<uuid>","name":"...","priority":"high|normal|low","content":"..."}
// Response: 201 {"id":"<uuid>","status":"queued"}
func (h *JobsHandler) Submit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSubmitBytes)

	var req job.SubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "text is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if _, err := model.ParsePriority(req.Priority); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp, err := job.Create(r.Context(), h.DB, req)
	if err != nil {
		switch {
		case errors.Is(err, job.ErrInvalidUserID):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, job.ErrUnknownUser):
			writeError(w, http.StatusBadRequest, "unknown user_id")
		default:
			log.Printf("create job: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to create job")
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// writeJSON sends v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeJobError turns the errors from the job package into HTTP replies.
func writeJobError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, job.ErrInvalidUserID), errors.Is(err, job.ErrInvalidJobID):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, job.ErrNotFound):
		writeError(w, http.StatusNotFound, "job not found")
	default:
		log.Printf("jobs: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// List handles GET /api/jobs?user_id=<uuid>
//
// Response: 200 with a JSON array of the user's jobs, newest first.
func (h *JobsHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}

	jobs, err := job.List(r.Context(), h.DB, userID)
	if err != nil {
		writeJobError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

// Get handles GET /api/jobs/{id}?user_id=<uuid>
//
// Response: 200 with one job (status, timestamps, and result once completed),
// or 404 if the job does not exist or belongs to another user.
func (h *JobsHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}

	detail, err := job.Get(r.Context(), h.DB, r.PathValue("id"), userID)
	if err != nil {
		writeJobError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}
