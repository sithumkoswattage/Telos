package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// JobStatus is the lifecycle state of a job.
//
//	            ┌──────────► cancelled ◄──────────┐
//	            │                                  │
//	queued ─────┴──► running ──► completed         │
//	   │                │                          │
//	   │                ├──────────────────────────┘
//	   └──► failed ◄────┘
//
// completed, failed and cancelled are terminal.
type JobStatus string

const (
	JobQueued    JobStatus = "queued"
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
	JobCancelled JobStatus = "cancelled"
)

// jobTransitions is the single source of truth for allowed job state changes.
var jobTransitions = map[JobStatus][]JobStatus{
	// queued -> failed covers a job that cannot be split (bad input).
	JobQueued:  {JobRunning, JobCancelled, JobFailed},
	JobRunning: {JobCompleted, JobFailed, JobCancelled},
	// Terminal states have no outgoing transitions.
	JobCompleted: nil,
	JobFailed:    nil,
	JobCancelled: nil,
}

// Valid reports whether s is a known job status.
func (s JobStatus) Valid() bool {
	_, ok := jobTransitions[s]
	return ok
}

// IsTerminal reports whether the job can no longer change state.
func (s JobStatus) IsTerminal() bool {
	return s.Valid() && len(jobTransitions[s]) == 0
}

// CanTransitionTo reports whether moving from s to next is allowed.
func (s JobStatus) CanTransitionTo(next JobStatus) bool {
	for _, allowed := range jobTransitions[s] {
		if allowed == next {
			return true
		}
	}
	return false
}

func (s JobStatus) String() string { return string(s) }

// Value implements driver.Valuer.
func (s JobStatus) Value() (driver.Value, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("invalid job status %q", string(s))
	}
	return string(s), nil
}

// Scan implements sql.Scanner.
func (s *JobStatus) Scan(src any) error {
	str, err := scanString(src)
	if err != nil {
		return fmt.Errorf("scan job status: %w", err)
	}
	v := JobStatus(str)
	if !v.Valid() {
		return fmt.Errorf("scan job status: unknown value %q", str)
	}
	*s = v
	return nil
}

// JobType identifies what kind of work a job does and therefore how the
// coordinator splits it into tasks.
type JobType string

const (
	JobTypeMatrixMultiply JobType = "matrix_multiply"
)

func (t JobType) Valid() bool {
	switch t {
	case JobTypeMatrixMultiply:
		return true
	}
	return false
}

// Job is a unit of work submitted by a user. It is split into Tasks.
//
// Progress is deliberately NOT a field: it is computed from the job's tasks
// (see ComputeProgress) so it can never go stale.
type Job struct {
	ID       string          `json:"id"`
	UserID   string          `json:"user_id"`
	Type     JobType         `json:"type"`
	Priority Priority        `json:"priority"`
	Status   JobStatus       `json:"status"`
	Input    json.RawMessage `json:"input,omitempty"`  // job-type-specific input, e.g. matrices A and B
	Result   json.RawMessage `json:"result,omitempty"` // combined result once completed
	Error    string          `json:"error,omitempty"`  // reason for failed / cancelled

	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"` // set on any terminal state
}

// NewJob builds a job in the queued state. ID generation is left to the caller
// (or the database) so this package has no dependencies.
func NewJob(id, userID string, typ JobType, priority Priority, input json.RawMessage, now time.Time) (*Job, error) {
	if !typ.Valid() {
		return nil, fmt.Errorf("invalid job type %q", typ)
	}
	if !priority.Valid() {
		return nil, fmt.Errorf("invalid priority %q", priority)
	}
	return &Job{
		ID:        id,
		UserID:    userID,
		Type:      typ,
		Priority:  priority,
		Status:    JobQueued,
		Input:     input,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// transition is the only place a job's Status is changed.
func (j *Job) transition(next JobStatus, now time.Time) error {
	if !j.Status.CanTransitionTo(next) {
		return &TransitionError{Entity: "job", ID: j.ID, From: string(j.Status), To: string(next)}
	}
	j.Status = next
	j.UpdatedAt = now
	if next.IsTerminal() {
		j.CompletedAt = &now
	}
	return nil
}

// Start marks the job as running (the scheduler picked it and it was split).
func (j *Job) Start(now time.Time) error {
	if err := j.transition(JobRunning, now); err != nil {
		return err
	}
	j.StartedAt = &now
	return nil
}

// Complete marks the job as completed with its combined result.
func (j *Job) Complete(result json.RawMessage, now time.Time) error {
	if err := j.transition(JobCompleted, now); err != nil {
		return err
	}
	j.Result = result
	return nil
}

// Fail marks the job as failed with a reason.
func (j *Job) Fail(reason string, now time.Time) error {
	if err := j.transition(JobFailed, now); err != nil {
		return err
	}
	j.Error = reason
	return nil
}

// Cancel marks the job as cancelled. Only queued or running jobs can be
// cancelled. The coordinator must stop handing out tasks of cancelled jobs.
func (j *Job) Cancel(reason string, now time.Time) error {
	if err := j.transition(JobCancelled, now); err != nil {
		return err
	}
	j.Error = reason
	return nil
}
