package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// TaskStatus is the lifecycle state of a task.
//
//	pending ──► assigned ──► running ──► done
//	   ▲           │            │
//	   │           │            ▼
//	   │           └────────► failed
//	   │                        │
//	   └──── requeue ◄──────────┘   (retry, if attempts remain)
//	   └──── requeue ◄── assigned / running   (worker lost: heartbeat timeout)
//
// done is terminal. failed is terminal only once attempts are used up.
type TaskStatus string

const (
	TaskPending  TaskStatus = "pending"
	TaskAssigned TaskStatus = "assigned"
	TaskRunning  TaskStatus = "running"
	TaskDone     TaskStatus = "done"
	TaskFailed   TaskStatus = "failed"
)

// taskTransitions is the single source of truth for allowed task state changes.
var taskTransitions = map[TaskStatus][]TaskStatus{
	TaskPending: {TaskAssigned},
	// assigned -> pending: worker went silent before starting.
	// assigned -> failed: worker rejected the task (e.g. bad payload).
	TaskAssigned: {TaskRunning, TaskPending, TaskFailed},
	// running -> pending: worker went silent mid-task.
	TaskRunning: {TaskDone, TaskFailed, TaskPending},
	// failed -> pending: retry. Guarded by attempts in Task.Requeue.
	TaskFailed: {TaskPending},
	TaskDone:   nil,
}

// Valid reports whether s is a known task status.
func (s TaskStatus) Valid() bool {
	_, ok := taskTransitions[s]
	return ok
}

// CanTransitionTo reports whether moving from s to next is allowed by the
// state graph. (Task.Requeue additionally checks remaining attempts.)
func (s TaskStatus) CanTransitionTo(next TaskStatus) bool {
	for _, allowed := range taskTransitions[s] {
		if allowed == next {
			return true
		}
	}
	return false
}

// IsActive reports whether a worker currently holds the task.
func (s TaskStatus) IsActive() bool {
	return s == TaskAssigned || s == TaskRunning
}

func (s TaskStatus) String() string { return string(s) }

// Value implements driver.Valuer.
func (s TaskStatus) Value() (driver.Value, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("invalid task status %q", string(s))
	}
	return string(s), nil
}

// Scan implements sql.Scanner.
func (s *TaskStatus) Scan(src any) error {
	str, err := scanString(src)
	if err != nil {
		return fmt.Errorf("scan task status: %w", err)
	}
	v := TaskStatus(str)
	if !v.Valid() {
		return fmt.Errorf("scan task status: unknown value %q", str)
	}
	*s = v
	return nil
}

// DefaultMaxAttempts is how many times a task may be tried before it is
// considered permanently failed.
const DefaultMaxAttempts = 3

// Task is one independent piece of a job, e.g. one row block of matrix A.
type Task struct {
	ID       string     `json:"id"`
	JobID    string     `json:"job_id"`
	Index    int        `json:"index"`    // position within the job, used to reassemble results in order
	Priority Priority   `json:"priority"` // inherited from the job
	Status   TaskStatus `json:"status"`

	WorkerID string `json:"worker_id,omitempty"` // empty unless assigned or running

	Attempts    int `json:"attempts"`     // number of times the task has been assigned
	MaxAttempts int `json:"max_attempts"` // after this many attempts a failure is final

	Input  json.RawMessage `json:"input,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"` // last failure / requeue reason

	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	AssignedAt  *time.Time `json:"assigned_at,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// NewTask builds a pending task that inherits its job's priority.
func NewTask(id string, job *Job, index int, input json.RawMessage, now time.Time) *Task {
	return &Task{
		ID:          id,
		JobID:       job.ID,
		Index:       index,
		Priority:    job.Priority,
		Status:      TaskPending,
		MaxAttempts: DefaultMaxAttempts,
		Input:       input,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func (t *Task) transition(next TaskStatus, now time.Time) error {
	if !t.Status.CanTransitionTo(next) {
		return &TransitionError{Entity: "task", ID: t.ID, From: string(t.Status), To: string(next)}
	}
	t.Status = next
	t.UpdatedAt = now
	return nil
}

// Assign hands a pending task to a worker and counts an attempt.
func (t *Task) Assign(workerID string, now time.Time) error {
	if workerID == "" {
		return fmt.Errorf("task %s: worker ID is required to assign", t.ID)
	}
	if t.Status == TaskPending && !t.HasAttemptsLeft() {
		return fmt.Errorf("task %s: no attempts left (%d/%d)", t.ID, t.Attempts, t.MaxAttempts)
	}
	if err := t.transition(TaskAssigned, now); err != nil {
		return err
	}
	t.WorkerID = workerID
	t.Attempts++
	t.AssignedAt = &now
	t.StartedAt = nil
	t.CompletedAt = nil
	t.Error = ""
	return nil
}

// Start records that the assigned worker began computing. The caller's
// workerID must match the one the task was assigned to.
func (t *Task) Start(workerID string, now time.Time) error {
	if err := t.checkOwner(workerID); err != nil {
		return err
	}
	if err := t.transition(TaskRunning, now); err != nil {
		return err
	}
	t.StartedAt = &now
	return nil
}

// Complete stores the result reported by the worker that owns the task.
// A duplicate or late report (task already done, or reassigned to another
// worker) is rejected, which is what the "duplicate results" test expects.
func (t *Task) Complete(workerID string, result json.RawMessage, now time.Time) error {
	if err := t.checkOwner(workerID); err != nil {
		return err
	}
	if err := t.transition(TaskDone, now); err != nil {
		return err
	}
	t.Result = result
	t.CompletedAt = &now
	return nil
}

// Fail records a failure reported by the owning worker. Whether the task is
// retried is decided by the caller via Requeue / HasAttemptsLeft.
func (t *Task) Fail(workerID, reason string, now time.Time) error {
	if err := t.checkOwner(workerID); err != nil {
		return err
	}
	if err := t.transition(TaskFailed, now); err != nil {
		return err
	}
	t.Error = reason
	t.CompletedAt = &now
	return nil
}

// Requeue puts the task back to pending so another worker can pick it up.
// It is used both for retries after a failure and for reclaiming tasks from
// a worker whose heartbeat timed out. It refuses when no attempts are left.
func (t *Task) Requeue(reason string, now time.Time) error {
	if !t.HasAttemptsLeft() {
		return fmt.Errorf("task %s: no attempts left (%d/%d): %w",
			t.ID, t.Attempts, t.MaxAttempts, ErrInvalidTransition)
	}
	if err := t.transition(TaskPending, now); err != nil {
		return err
	}
	t.WorkerID = ""
	t.Error = reason
	t.AssignedAt = nil
	t.StartedAt = nil
	t.CompletedAt = nil
	return nil
}

// HasAttemptsLeft reports whether the task may be assigned again.
func (t *Task) HasAttemptsLeft() bool {
	return t.Attempts < t.MaxAttempts
}

// IsPermanentlyFailed reports whether the task failed and will not be retried.
func (t *Task) IsPermanentlyFailed() bool {
	return t.Status == TaskFailed && !t.HasAttemptsLeft()
}

func (t *Task) checkOwner(workerID string) error {
	if t.WorkerID == "" || t.WorkerID != workerID {
		return fmt.Errorf("task %s: not owned by worker %q (owner %q): %w",
			t.ID, workerID, t.WorkerID, ErrInvalidTransition)
	}
	return nil
}
