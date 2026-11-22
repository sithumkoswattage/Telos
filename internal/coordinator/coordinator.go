// Package coordinator is the server's background loop (TEL-80, TEL-37).
//
// Every second it does two things:
//
//  1. Start queued jobs: a text job is split into chunks (see
//     textjob.Split), each chunk becomes one task in the queue, and the job
//     moves to "running". Several workers can then work on one job at once.
//  2. Finish running jobs: when all of a job's tasks are done, their results
//     are combined into the job's result and the job becomes "completed". If
//     any task failed with no retries left, the job becomes "failed".
//
// Workers never touch the jobs table; they only see tasks.
package coordinator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"telos/internal/events"
	"telos/internal/model"
	"telos/internal/queue"
	"telos/internal/textjob"
)

// Coordinator moves jobs through their states.
type Coordinator struct {
	DB    *sql.DB
	Queue *queue.Queue
	// Chunks is the most tasks a text job is split into. Short texts get
	// fewer (see textjob.MinChunkSize).
	Chunks int
}

// New returns a Coordinator using db.
func New(db *sql.DB) *Coordinator {
	return &Coordinator{DB: db, Queue: queue.New(db), Chunks: textjob.DefaultChunks}
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

// startQueuedJobs splits each queued job into pending tasks.
func (c *Coordinator) startQueuedJobs(ctx context.Context) error {
	jobs, err := c.Queue.ListQueuedJobs(ctx, 20)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		var typ string
		var input []byte
		err := c.DB.QueryRowContext(ctx,
			`SELECT type, input FROM jobs WHERE id = $1`, j.ID).Scan(&typ, &input)
		if err != nil {
			return err
		}
		inputs := c.split(model.JobType(typ), input)
		// EnqueueTasks saves all the tasks and starts the job in one go.
		_, err = c.Queue.EnqueueTasks(ctx, j.ID, inputs)
		if errors.Is(err, queue.ErrConflict) {
			continue // someone else started it in the meantime; that's fine
		}
		if err != nil {
			return err
		}
		log.Printf("Job %s started as %d task(s)", j.ID, len(inputs))
		events.Job(ctx, c.DB, j.ID, "", events.JobStarted,
			fmt.Sprintf("Split into %d task(s)", len(inputs)))
	}
	return nil
}

// split turns a job's input into one input per task.
func (c *Coordinator) split(typ model.JobType, input json.RawMessage) []json.RawMessage {
	if typ == model.JobTypeTextProcessing {
		if inputs, err := textjob.SplitInput(input, c.Chunks); err == nil {
			return inputs
		}
		// Bad input: send it as one task anyway. The worker reports the
		// error, so the job fails with a clear reason.
	}
	return []json.RawMessage{input}
}

// finishRunningJobs completes or fails running jobs whose tasks are finished.
func (c *Coordinator) finishRunningJobs(ctx context.Context) error {
	rows, err := c.DB.QueryContext(ctx, `SELECT id::text, type FROM jobs WHERE status = 'running'`)
	if err != nil {
		return err
	}
	type runningJob struct {
		id  string
		typ model.JobType
	}
	var running []runningJob
	for rows.Next() {
		var id, typ string
		if err := rows.Scan(&id, &typ); err != nil {
			rows.Close()
			return err
		}
		running = append(running, runningJob{id, model.JobType(typ)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, j := range running {
		id := j.id
		tasks, err := c.Queue.Tasks(ctx, id) // in task order
		if err != nil {
			return err
		}
		switch model.DecideJobOutcome(tasks) {
		case model.OutcomeCompleted:
			result, cerr := combine(j.typ, tasks)
			if cerr != nil {
				err = c.finish(ctx, id, "failed", nil, "could not combine results: "+cerr.Error())
			} else {
				err = c.finish(ctx, id, "completed", result, "")
			}
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
	ret, err := c.DB.ExecContext(ctx, `
		UPDATE jobs
		SET status = $2, result = $3, error = $4, completed_at = now(), updated_at = now()
		WHERE id = $1 AND status = 'running'`,
		jobID, status, res, reason)
	if err != nil {
		return err
	}
	// Only log the event if this call really finished the job (not a repeat).
	if n, _ := ret.RowsAffected(); n == 1 {
		log.Printf("Job %s %s", jobID, status)
		if status == "completed" {
			events.Job(ctx, c.DB, jobID, "", events.JobCompleted, "Job completed")
		} else {
			events.Job(ctx, c.DB, jobID, "", events.JobFailed, "Job failed: "+reason)
		}
	}
	return nil
}

// combine joins the results of a job's tasks (in task order) into the
// job's result.
func combine(typ model.JobType, tasks []*model.Task) (json.RawMessage, error) {
	if typ == model.JobTypeTextProcessing {
		inputs := make([]json.RawMessage, len(tasks))
		results := make([]json.RawMessage, len(tasks))
		for i, t := range tasks {
			inputs[i], results[i] = t.Input, t.Result
		}
		return textjob.CombineJSON(inputs, results)
	}
	if len(tasks) == 1 {
		return tasks[0].Result, nil // nothing to combine
	}
	return nil, fmt.Errorf("cannot combine %d tasks of a %s job", len(tasks), typ)
}

func failureReason(tasks []*model.Task) string {
	for _, t := range tasks {
		if t.IsPermanentlyFailed() {
			return "task failed after all retries: " + t.Error
		}
	}
	return "task failed"
}
