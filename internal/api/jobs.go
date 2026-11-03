package api

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"telos/internal/job"
)

type JobsHandler struct {
	DB *sql.DB
}

func (h *JobsHandler) Submit(w http.ResponseWriter, r *http.Request) {
	var req job.SubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}
	if req.UserID == 0 {
		http.Error(w, `{"error":"user_id is required"}`, http.StatusBadRequest)
		return
	}

	resp, err := job.Create(r.Context(), h.DB, req)
	if err != nil {
		http.Error(w, `{"error":"failed to create job"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}
