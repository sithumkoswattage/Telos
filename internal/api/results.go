package api

// TEL-41: result retrieval and the job's event timeline.
//
//	GET /api/jobs/{id}/result              -> the result of a completed job
//	GET /api/jobs/{id}/result?download=1   -> the same, as a .json file to save
//	GET /api/jobs/{id}/events              -> what happened to the job, oldest first
//
// Like GET /api/jobs/{id}, both only work for the job's owner: anyone else
// gets 404, so they cannot even tell that the job exists.

import (
	"encoding/json"
	"net/http"
	"time"

	"telos/internal/events"
	"telos/internal/job"
)

// ResultResponse is the reply of GET /api/jobs/{id}/result.
type ResultResponse struct {
	JobID       string          `json:"job_id"`
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	CompletedAt *time.Time      `json:"completed_at"`
	Result      json.RawMessage `json:"result"`
}

// Result handles GET /api/jobs/{id}/result.
//
//	200 completed job, with its result
//	409 job not finished yet, or it failed / was cancelled (no result)
//	404 no such job for this user
func (h *JobsHandler) Result(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w)
	if !ok {
		return
	}
	d, err := job.Get(r.Context(), h.DB, r.PathValue("id"), userID)
	if err != nil {
		writeJobError(w, err)
		return
	}

	switch {
	case d.Status == "completed" && len(d.Result) > 0:
		// fall through to the reply below
	case d.Status == "failed":
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "job failed, so it has no result: " + d.Error, "status": d.Status})
		return
	case d.Status == "cancelled":
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "job was cancelled, so it has no result", "status": d.Status})
		return
	default: // queued or running
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "job is not finished yet", "status": d.Status})
		return
	}

	if r.URL.Query().Get("download") != "" {
		// Tells the browser to save the reply as a file instead of showing it.
		w.Header().Set("Content-Disposition",
			`attachment; filename="telos-result-`+d.ID[:8]+`.json"`)
	}
	writeJSON(w, http.StatusOK, ResultResponse{
		JobID:       d.ID,
		Name:        d.Name,
		Type:        d.Type,
		CompletedAt: d.CompletedAt,
		Result:      d.Result,
	})
}

// Events handles GET /api/jobs/{id}/events.
func (h *JobsHandler) Events(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w)
	if !ok {
		return
	}
	// job.Get checks the job exists and belongs to this user.
	d, err := job.Get(r.Context(), h.DB, r.PathValue("id"), userID)
	if err != nil {
		writeJobError(w, err)
		return
	}
	list, err := events.ForJob(r.Context(), h.DB, d.ID)
	if err != nil {
		writeJobError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}
