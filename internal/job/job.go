package job

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Job struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Queue     string    `json:"queue"`
	Priority  string    `json:"priority"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// SubmitRequest is the JSON body the client sends to POST /jobs.
type SubmitRequest struct {
	UserID   int            `json:"user_id"`
	Name     string         `json:"name"`
	Queue    string         `json:"queue"`
	Priority string         `json:"priority"`
	Payload  map[string]any `json:"payload"`
}

// SubmitResponse is what we return immediately — id + "queued".
type SubmitResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// Create inserts a new job row and returns the generated ID.
func Create(ctx context.Context, db *sql.DB, req SubmitRequest) (SubmitResponse, error) {
	if req.Queue == "" {
		req.Queue = "default"
	}
	if req.Priority == "" {
		req.Priority = "normal"
	}

	id := fmt.Sprintf("J-%s", uuid.New().String()[:8])

	var status string
	err := db.QueryRowContext(ctx,
		`INSERT INTO jobs (id, user_id, name, queue, priority, payload)
         VALUES ($1, $2, $3, $4, $5, $6)
         RETURNING status`,
		id, req.UserID, req.Name, req.Queue, req.Priority, req.Payload,
	).Scan(&status)
	if err != nil {
		return SubmitResponse{}, err
	}
	return SubmitResponse{ID: id, Status: status}, nil
}
