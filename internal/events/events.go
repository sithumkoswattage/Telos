// Package events is the job event log (TEL-41).
//
// Every important moment in a job's life is saved as one row in the events
// table: submitted, split into tasks, each task assigned/started/done, and
// the job completed or failed. The job's timeline can then be shown later
// (GET /api/jobs/{id}/events), even after the server restarts.
//
// Writing an event is "best effort": if it fails, the error is printed to the
// server log and the real work carries on. A missing log line is much less
// harmful than a job that stops because its log could not be written.
package events

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// Event types. The part before the dot says what the event is about.
const (
	JobSubmitted = "job.submitted"
	JobStarted   = "job.started" // split into tasks
	JobCompleted = "job.completed"
	JobFailed    = "job.failed"

	TaskAssigned  = "task.assigned"
	TaskStarted   = "task.started"
	TaskCompleted = "task.completed"
	TaskRetry     = "task.retry"  // failed, but will be tried again
	TaskFailed    = "task.failed" // failed with no attempts left

	WorkerRegistered = "worker.registered"
)

// Entry is one event as shown in a job's timeline.
type Entry struct {
	Type      string    `json:"type"`
	Message   string    `json:"message"`
	Task      *int      `json:"task,omitempty"`   // task index, for task events
	Worker    string    `json:"worker,omitempty"` // worker name, if a worker was involved
	CreatedAt time.Time `json:"created_at"`
}

// Job records an event about a job. workerID may be "".
func Job(ctx context.Context, db *sql.DB, jobID, workerID, typ, message string) {
	_, err := db.ExecContext(ctx, `
		INSERT INTO events (job_id, worker_id, type, message)
		VALUES (NULLIF($1, '')::uuid, NULLIF($2, '')::uuid, $3, $4)`,
		jobID, workerID, typ, message)
	logIfFailed(err, typ)
}

// Task records an event about a task. The job it belongs to is looked up
// from the tasks table, so callers only need the task ID.
func Task(ctx context.Context, db *sql.DB, taskID, workerID, typ, message string) {
	_, err := db.ExecContext(ctx, `
		INSERT INTO events (job_id, task_id, worker_id, type, message)
		SELECT job_id, id, NULLIF($2, '')::uuid, $3, $4
		FROM tasks WHERE id = $1`,
		taskID, workerID, typ, message)
	logIfFailed(err, typ)
}

// Worker records an event about a worker that is not tied to a job.
func Worker(ctx context.Context, db *sql.DB, workerID, typ, message string) {
	Job(ctx, db, "", workerID, typ, message)
}

// ForJob returns a job's events, oldest first.
func ForJob(ctx context.Context, db *sql.DB, jobID string) ([]Entry, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT e.type, e.message, t.task_index, COALESCE(w.name, ''), e.created_at
		FROM events e
		LEFT JOIN tasks t   ON t.id = e.task_id
		LEFT JOIN workers w ON w.id = e.worker_id
		WHERE e.job_id = $1
		ORDER BY e.created_at, e.id`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []Entry{} // [] in JSON, not null
	for rows.Next() {
		var e Entry
		var index sql.NullInt64
		if err := rows.Scan(&e.Type, &e.Message, &index, &e.Worker, &e.CreatedAt); err != nil {
			return nil, err
		}
		if index.Valid {
			i := int(index.Int64)
			e.Task = &i
		}
		list = append(list, e)
	}
	return list, rows.Err()
}

func logIfFailed(err error, typ string) {
	if err != nil {
		log.Printf("events: could not record %s: %v", typ, err)
	}
}
