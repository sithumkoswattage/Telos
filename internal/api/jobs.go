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
