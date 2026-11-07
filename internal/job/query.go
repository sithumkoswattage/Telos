package job

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// maxListLimit caps how many jobs GET /api/jobs returns in one go.
const maxListLimit = 100

// ErrInvalidJobID is returned when the job id is not a valid UUID.
var ErrInvalidJobID = errors.New("job id must be a valid UUID")

// ErrNotFound is returned when no job with that id belongs to that user.
// It is deliberately the same error for "does not exist" and "belongs to
// someone else", so a user cannot discover other people's job ids.
var ErrNotFound = errors.New("job not found")

// Summary is one row of the job list.
type Summary struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Queue     string    `json:"queue"`
	Priority  string    `json:"priority"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// Detail is one job with everything a status page needs. Result is only set
// once the job has completed.
type Detail struct {
	Summary
	UpdatedAt   time.Time       `json:"updated_at"`
	StartedAt   *time.Time      `json:"started_at,omitempty"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
	Error       string          `json:"error,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
}

// List returns the user's jobs, newest first (at most maxListLimit).
func List(ctx context.Context, db *sql.DB, userID string) ([]Summary, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return nil, ErrInvalidUserID
	}

	rows, err := db.QueryContext(ctx,
		`SELECT id::text, name, type, queue, priority, status, created_at
		 FROM jobs
		 WHERE user_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2`,
		userID, maxListLimit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := []Summary{} // empty list, not nil, so the JSON is [] and not null
	for rows.Next() {
		var s Summary
		if err := rows.Scan(&s.ID, &s.Name, &s.Type, &s.Queue, &s.Priority, &s.Status, &s.CreatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return jobs, nil
}

// Get returns one job, but only if it belongs to userID.
func Get(ctx context.Context, db *sql.DB, id, userID string) (Detail, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return Detail{}, ErrInvalidUserID
	}
	if _, err := uuid.Parse(id); err != nil {
		return Detail{}, ErrInvalidJobID
	}

	var (
		d         Detail
		started   sql.NullTime
		completed sql.NullTime
		result    sql.NullString
	)
	err := db.QueryRowContext(ctx,
		`SELECT id::text, name, type, queue, priority, status, created_at,
		        updated_at, started_at, completed_at, error, result::text
		 FROM jobs
		 WHERE id = $1 AND user_id = $2`,
		id, userID,
	).Scan(&d.ID, &d.Name, &d.Type, &d.Queue, &d.Priority, &d.Status, &d.CreatedAt,
		&d.UpdatedAt, &started, &completed, &d.Error, &result)
	if errors.Is(err, sql.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, err
	}

	if started.Valid {
		t := started.Time
		d.StartedAt = &t
	}
	if completed.Valid {
		t := completed.Time
		d.CompletedAt = &t
	}
	if result.Valid {
		d.Result = json.RawMessage(result.String)
	}
	return d, nil
}
