// Package queue is the persistent job and task queue (TEL-21).
//
// The queue lives entirely in PostgreSQL: queued jobs are rows in `jobs` with
// status 'queued', and the work queue is the rows in `tasks` with status
// 'pending'. Because nothing is kept only in memory, the queue survives a
// coordinator restart, and several coordinators or workers can use it at once.
package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"telos/internal/model"
)

var (
	// ErrEmpty means there is no task available to claim right now.
	ErrEmpty = errors.New("queue: no task available")
	// ErrNotFound means the job or task does not exist.
	ErrNotFound = errors.New("queue: not found")
	// ErrConflict means the job or task is not in a state that allows the
	// operation, or the worker does not own the task.
	ErrConflict = fmt.Errorf("queue: %w", model.ErrInvalidTransition)
)

// Queue is a PostgreSQL-backed job and task queue.
type Queue struct {
	db *sql.DB
}

// New returns a Queue that uses db.
func New(db *sql.DB) *Queue {
	return &Queue{db: db}
}

// priorityOrder sorts high, normal, low. Must match model.Priority.Rank.
const priorityOrder = `CASE %s WHEN 'high' THEN 0 WHEN 'normal' THEN 1 ELSE 2 END`

const taskColumns = `id, job_id, task_index, priority, status, worker_id,
	attempts, max_attempts, input, result, error,
	created_at, updated_at, assigned_at, started_at, completed_at`

// QueuedJob is a job waiting to be split into tasks.
type QueuedJob struct {
	ID        string
	Priority  model.Priority
	CreatedAt time.Time
}

// ListQueuedJobs returns up to limit queued jobs, most urgent first and
// oldest first within a priority.
func (q *Queue) ListQueuedJobs(ctx context.Context, limit int) ([]QueuedJob, error) {
	rows, err := q.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, priority, created_at
		FROM jobs
		WHERE status = 'queued'
		ORDER BY `+priorityOrder+`, created_at, id
		LIMIT $1`, "priority"), limit)
	if err != nil {
		return nil, fmt.Errorf("list queued jobs: %w", err)
	}
	defer rows.Close()

	var jobs []QueuedJob
	for rows.Next() {
		var j QueuedJob
		if err := rows.Scan(&j.ID, &j.Priority, &j.CreatedAt); err != nil {
			return nil, fmt.Errorf("list queued jobs: %w", err)
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// EnqueueTasks splits a queued job: it inserts one pending task per input and
// moves the job to running, all in one transaction.
func (q *Queue) EnqueueTasks(ctx context.Context, jobID string, inputs []json.RawMessage) ([]*model.Task, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("enqueue tasks for job %s: at least one task is required", jobID)
	}

	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("enqueue tasks: %w", err)
	}
	defer tx.Rollback() // does nothing after Commit

	// Lock the job row so two coordinators can't split the same job.
	var status model.JobStatus
	var priority model.Priority
	err = tx.QueryRowContext(ctx,
		`SELECT status, priority FROM jobs WHERE id = $1 FOR UPDATE`, jobID,
	).Scan(&status, &priority)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("job %s: %w", jobID, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("enqueue tasks: %w", err)
	}
	if !status.CanTransitionTo(model.JobRunning) {
		return nil, fmt.Errorf("job %s is %s, not queued: %w", jobID, status, ErrConflict)
	}

	tasks := make([]*model.Task, 0, len(inputs))
	for i, in := range inputs {
		t, err := scanTask(tx.QueryRowContext(ctx, `
			INSERT INTO tasks (job_id, task_index, priority, input)
			VALUES ($1, $2, $3, $4)
			RETURNING `+taskColumns,
			jobID, i, priority, jsonOrNull(in)))
		if err != nil {
			return nil, fmt.Errorf("insert task %d: %w", i, err)
		}
		tasks = append(tasks, t)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE jobs SET status = 'running', started_at = now(), updated_at = now()
		WHERE id = $1`, jobID); err != nil {
		return nil, fmt.Errorf("start job: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("enqueue tasks: %w", err)
	}
	return tasks, nil
}

// ClaimTask gives workerID the next pending task: highest priority first,
// then oldest. Only tasks of running jobs are handed out.
func (q *Queue) ClaimTask(ctx context.Context, workerID string) (*model.Task, error) {
	return q.claim(ctx, workerID, "")
}

// ClaimTaskForJob is like ClaimTask but only takes a task of jobID. The
// Round Robin scheduler uses it to give each job one task per turn.
func (q *Queue) ClaimTaskForJob(ctx context.Context, workerID, jobID string) (*model.Task, error) {
	if jobID == "" {
		return nil, fmt.Errorf("claim task: job ID is required")
	}
	return q.claim(ctx, workerID, jobID)
}

func (q *Queue) claim(ctx context.Context, workerID, jobID string) (*model.Task, error) {
	if workerID == "" {
		return nil, fmt.Errorf("claim task: worker ID is required")
	}
	t, err := scanTask(q.db.QueryRowContext(ctx, fmt.Sprintf(`
		UPDATE tasks SET
			status = 'assigned', worker_id = $1, attempts = attempts + 1,
			assigned_at = now(), started_at = NULL, completed_at = NULL,
			error = '', updated_at = now()
		WHERE id = (
			SELECT t.id
			FROM tasks t JOIN jobs j ON j.id = t.job_id
			WHERE t.status = 'pending'
			  AND t.attempts < t.max_attempts
			  AND j.status = 'running'
			  AND ($2 = '' OR t.job_id::text = $2)
			ORDER BY `+priorityOrder+`, t.created_at, t.task_index
			LIMIT 1
			FOR UPDATE OF t SKIP LOCKED
		)
		RETURNING `+taskColumns, "t.priority"), workerID, jobID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEmpty
	}
	if err != nil {
		return nil, fmt.Errorf("claim task: %w", err)
	}
	return t, nil
}

// StartTask records that the worker began computing (assigned -> running).
func (q *Queue) StartTask(ctx context.Context, taskID, workerID string) error {
	return q.execOwned(ctx, taskID, workerID, `
		UPDATE tasks SET status = 'running', started_at = now(), updated_at = now()
		WHERE id = $1 AND worker_id = $2 AND status = 'assigned'`)
}

// CompleteTask stores the worker's result (assigned/running -> done).
// A duplicate result, or a late result from a worker that lost the task,
// changes nothing and returns ErrConflict.
func (q *Queue) CompleteTask(ctx context.Context, taskID, workerID string, result json.RawMessage) error {
	return q.execOwned(ctx, taskID, workerID, `
		UPDATE tasks SET status = 'done', result = $3,
			completed_at = now(), updated_at = now()
		WHERE id = $1 AND worker_id = $2 AND status IN ('assigned', 'running')`,
		jsonOrNull(result))
}

// FailTask records a failure. The task goes back to pending if it has
// attempts left, otherwise it stays failed.
func (q *Queue) FailTask(ctx context.Context, taskID, workerID, reason string) (retried bool, err error) {
	var status model.TaskStatus
	err = q.db.QueryRowContext(ctx, `
		UPDATE tasks SET
			status       = CASE WHEN attempts < max_attempts THEN 'pending' ELSE 'failed' END,
			worker_id    = CASE WHEN attempts < max_attempts THEN NULL ELSE worker_id END,
			assigned_at  = CASE WHEN attempts < max_attempts THEN NULL ELSE assigned_at END,
			started_at   = CASE WHEN attempts < max_attempts THEN NULL ELSE started_at END,
			completed_at = CASE WHEN attempts < max_attempts THEN NULL ELSE now() END,
			error = $3, updated_at = now()
		WHERE id = $1 AND worker_id = $2 AND status IN ('assigned', 'running')
		RETURNING status`, taskID, workerID, reason).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, q.whyNoRows(ctx, taskID, workerID)
	}
	if err != nil {
		return false, fmt.Errorf("fail task: %w", err)
	}
	return status == model.TaskPending, nil
}

// RequeueWorkerTasks reclaims every task held by a worker that stopped
// sending heartbeats. Tasks with attempts left go back to pending; the rest
// become failed.
func (q *Queue) RequeueWorkerTasks(ctx context.Context, workerID, reason string) (requeued, failed int, err error) {
	rows, err := q.db.QueryContext(ctx, `
		UPDATE tasks SET
			status       = CASE WHEN attempts < max_attempts THEN 'pending' ELSE 'failed' END,
			worker_id    = CASE WHEN attempts < max_attempts THEN NULL ELSE worker_id END,
			assigned_at  = CASE WHEN attempts < max_attempts THEN NULL ELSE assigned_at END,
			started_at   = CASE WHEN attempts < max_attempts THEN NULL ELSE started_at END,
			completed_at = CASE WHEN attempts < max_attempts THEN NULL ELSE now() END,
			error = $2, updated_at = now()
		WHERE worker_id = $1 AND status IN ('assigned', 'running')
		RETURNING status`, workerID, reason)
	if err != nil {
		return 0, 0, fmt.Errorf("requeue worker tasks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s model.TaskStatus
		if err := rows.Scan(&s); err != nil {
			return 0, 0, fmt.Errorf("requeue worker tasks: %w", err)
		}
		if s == model.TaskPending {
			requeued++
		} else {
			failed++
		}
	}
	return requeued, failed, rows.Err()
}

// Tasks returns all tasks of a job in index order.
func (q *Queue) Tasks(ctx context.Context, jobID string) ([]*model.Task, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT `+taskColumns+` FROM tasks WHERE job_id = $1 ORDER BY task_index`, jobID)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()
	var tasks []*model.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("list tasks: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// Progress counts a job's tasks by status. Computed on demand, never stored.
func (q *Queue) Progress(ctx context.Context, jobID string) (model.Progress, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT status, COUNT(*) FROM tasks WHERE job_id = $1 GROUP BY status`, jobID)
	if err != nil {
		return model.Progress{}, fmt.Errorf("progress: %w", err)
	}
	defer rows.Close()
	counts := map[model.TaskStatus]int{}
	for rows.Next() {
		var s model.TaskStatus
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return model.Progress{}, fmt.Errorf("progress: %w", err)
		}
		counts[s] = n
	}
	if err := rows.Err(); err != nil {
		return model.Progress{}, fmt.Errorf("progress: %w", err)
	}
	return model.ProgressFromCounts(counts), nil
}

// execOwned runs a task UPDATE that must hit exactly one row owned by
// workerID, and explains why when it hits none.
func (q *Queue) execOwned(ctx context.Context, taskID, workerID, query string, extra ...any) error {
	args := append([]any{taskID, workerID}, extra...)
	res, err := q.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update task %s: %w", taskID, err)
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	return q.whyNoRows(ctx, taskID, workerID)
}

func (q *Queue) whyNoRows(ctx context.Context, taskID, workerID string) error {
	var status model.TaskStatus
	var owner sql.NullString
	err := q.db.QueryRowContext(ctx,
		`SELECT status, worker_id FROM tasks WHERE id = $1`, taskID).Scan(&status, &owner)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("task %s: %w", taskID, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("task %s: %w", taskID, err)
	}
	return fmt.Errorf("task %s is %s (owner %q), worker %q cannot change it: %w",
		taskID, status, owner.String, workerID, ErrConflict)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(r rowScanner) (*model.Task, error) {
	var t model.Task
	var worker sql.NullString
	var input, result []byte
	var assigned, started, completed sql.NullTime
	err := r.Scan(&t.ID, &t.JobID, &t.Index, &t.Priority, &t.Status, &worker,
		&t.Attempts, &t.MaxAttempts, &input, &result, &t.Error,
		&t.CreatedAt, &t.UpdatedAt, &assigned, &started, &completed)
	if err != nil {
		return nil, err
	}
	t.WorkerID = worker.String
	t.Input = json.RawMessage(input)
	t.Result = json.RawMessage(result)
	t.AssignedAt = timePtr(assigned)
	t.StartedAt = timePtr(started)
	t.CompletedAt = timePtr(completed)
	return &t, nil
}

func timePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

// jsonOrNull passes JSON to the driver as text, or NULL when empty.
func jsonOrNull(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}
