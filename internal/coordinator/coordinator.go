// Package coordinator is the server's background loop (TEL-80).
//
// Every second it does two things:
//
//  1. Start queued jobs: each queued job becomes one task in the queue, and
//     the job moves to "running". (Splitting a job into several tasks for
//     several workers comes later, in TEL-37.)
//  2. Finish running jobs: when a job's task is done, the task's result is
//     copied onto the job and the job becomes "completed". If the task failed
//     with no retries left, the job becomes "failed".
//
// Workers never touch the jobs table; they only see tasks.
package coordinator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"time"

	"telos/internal/model"
	"telos/internal/queue"
)

// Coordinator moves jobs through their states.
type Coordinator struct {
	DB    *sql.DB
	Queue *queue.Queue
}

// New returns a Coordinator using db.
func New(db *sql.DB) *Coordinator {
	return &Coordinator{DB: db, Queue: queue.New(db)}
}

// Run calls Tick every interval until ctx is cancelled. Start it with
// `go c.Run(ctx, time.Second)` so it runs in the background.
func (c *Coordinator) Run(ctx context.Context, interval time.Duration) {
	log.Printf("Coordinator started (checking every %s)", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := c.Tick(ctx); err != nil {
			log.Printf("coordinator: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Tick does one round of work. Tests call it directly.
func (c *Coordinator) Tick(ctx context.Context) error {
	if err := c.startQueuedJobs(ctx); err != nil {
		return err
	}
	return c.finishRunningJobs(ctx)
}

// startQueuedJobs turns each queued job into one pending task.
func (c *Coordinator) startQueuedJobs(ctx context.Context) error {
	jobs, err := c.Queue.ListQueuedJobs(ctx, 20)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		var input []byte
		err := c.DB.QueryRowContext(ctx, `SELECT input FROM jobs WHERE id = $1`, j.ID).Scan(&input)
		if err != nil {
			return err
		}
		// The whole job is one task, with the same input as the job.
		_, err = c.Queue.EnqueueTasks(ctx, j.ID, []json.RawMessage{input})
		if errors.Is(err, queue.ErrConflict) {
			continue // someone else started it in the meantime; that's fine
		}
		if err != nil {
			return err
		}
		log.Printf("Job %s started", j.ID)
	}
	return nil
}

// finishRunningJobs completes or fails running jobs whose tasks are finished.
func (c *Coordinator) finishRunningJobs(ctx context.Context) error {
	rows, err := c.DB.QueryContext(ctx, `SELECT id::text FROM jobs WHERE status = 'running'`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, id := range ids {
		tasks, err := c.Queue.Tasks(ctx, id)
		if err != nil {
			return err
		}
		switch model.DecideJobOutcome(tasks) {
		case model.OutcomeCompleted:
			// One task per job for now, so its result is the job's result.
			err = c.finish(ctx, id, "completed", tasks[0].Result, "")
		case model.OutcomeFailed:
			err = c.finish(ctx, id, "failed", nil, failureReason(tasks))
		default:
			continue // still in progress
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// finish moves a running job to completed or failed. The status check in
// WHERE makes it safe to call twice: the second call changes nothing.
func (c *Coordinator) finish(ctx context.Context, jobID, status string, result json.RawMessage, reason string) error {
	var res any // NULL when there is no result
	if len(result) > 0 {
		res = string(result)
	}
	_, err := c.DB.ExecContext(ctx, `
		UPDATE jobs
		SET status = $2, result = $3, error = $4, completed_at = now(), updated_at = now()
		WHERE id = $1 AND status = 'running'`,
		jobID, status, res, reason)
	if err == nil {
		log.Printf("Job %s %s", jobID, status)
	}
	return err
}

func failureReason(tasks []*model.Task) string {
	for _, t := range tasks {
		if t.IsPermanentlyFailed() {
			return "task failed after all retries: " + t.Error
		}
	}
	return "task failed"
}
