package job

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"telos/internal/model"
)

// DefaultQueue is the queue text-processing jobs go to unless the client
// asks for another one.
const DefaultQueue = "text-processing"

// ErrUnknownUser is returned when user_id does not match a row in users.
var ErrUnknownUser = errors.New("unknown user")

// ErrInvalidUserID is returned when user_id is not a valid UUID.
var ErrInvalidUserID = errors.New("user_id must be a valid UUID")

// SubmitRequest is the JSON body the client sends to POST /api/jobs.
//
// The text to process is sent as Content, whether the user typed or pasted it
// or uploaded a .txt file (the frontend reads the file and sends its text).
// The server stores it in the job's input column as {"content": "..."}.
type SubmitRequest struct {
	UserID   string `json:"user_id"` // UUID. TODO: take from the JWT once login (TEL-27) exists.
	Name     string `json:"name"`
	Queue    string `json:"queue"`    // optional, default "text-processing"
	Priority string `json:"priority"` // optional: high | normal | low, default normal
	Content  string `json:"content"`  // the text to process
}

// SubmitResponse is what we return immediately — id + "queued".
type SubmitResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// Create inserts a new text_processing job in the queued state. The database
// generates the UUID, which is returned as the job's real ID.
func Create(ctx context.Context, db *sql.DB, req SubmitRequest) (SubmitResponse, error) {
	if _, err := uuid.Parse(req.UserID); err != nil {
		return SubmitResponse{}, ErrInvalidUserID
	}
	priority, err := model.ParsePriority(req.Priority) // "" -> normal
	if err != nil {
		return SubmitResponse{}, err
	}
	queue := req.Queue
	if queue == "" {
		queue = DefaultQueue
	}

	input, err := json.Marshal(map[string]string{"content": req.Content})
	if err != nil {
		return SubmitResponse{}, err
	}

	var resp SubmitResponse
	err = db.QueryRowContext(ctx,
		`INSERT INTO jobs (user_id, name, type, queue, priority, input)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id::text, status`,
		req.UserID, req.Name, string(model.JobTypeTextProcessing), queue, string(priority), input,
	).Scan(&resp.ID, &resp.Status)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" { // foreign_key_violation
			return SubmitResponse{}, ErrUnknownUser
		}
		return SubmitResponse{}, err
	}
	return resp, nil
}
