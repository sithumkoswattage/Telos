package model

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func newTestJob(t *testing.T) *Job {
	t.Helper()
	j, err := NewJob("job-1", "user-1", JobTypeMatrixMultiply, PriorityHigh, json.RawMessage(`{}`), t0)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

// ---------- Job ----------

func TestNewJobStartsQueued(t *testing.T) {
	j := newTestJob(t)
	if j.Status != JobQueued {
		t.Fatalf("status = %s, want queued", j.Status)
	}
}

func TestNewJobRejectsBadInput(t *testing.T) {
	if _, err := NewJob("j", "u", "bogus", PriorityNormal, nil, t0); err == nil {
		t.Error("expected error for invalid job type")
	}
	if _, err := NewJob("j", "u", JobTypeMatrixMultiply, "urgent", nil, t0); err == nil {
		t.Error("expected error for invalid priority")
	}
}

func TestJobTransitionTable(t *testing.T) {
	all := []JobStatus{JobQueued, JobRunning, JobCompleted, JobFailed, JobCancelled}
	allowed := map[[2]JobStatus]bool{
		{JobQueued, JobRunning}:    true,
		{JobQueued, JobCancelled}:  true,
		{JobQueued, JobFailed}:     true,
		{JobRunning, JobCompleted}: true,
		{JobRunning, JobFailed}:    true,
		{JobRunning, JobCancelled}: true,
	}
	for _, from := range all {
		for _, to := range all {
			want := allowed[[2]JobStatus{from, to}]
			if got := from.CanTransitionTo(to); got != want {
				t.Errorf("%s -> %s: got %v, want %v", from, to, got, want)
			}
		}
	}
}

func TestJobHappyPath(t *testing.T) {
	j := newTestJob(t)
	t1 := t0.Add(time.Second)
	if err := j.Start(t1); err != nil {
		t.Fatal(err)
	}
	if j.StartedAt == nil || !j.StartedAt.Equal(t1) {
		t.Error("StartedAt not set")
	}
	t2 := t0.Add(2 * time.Second)
	if err := j.Complete(json.RawMessage(`[[1]]`), t2); err != nil {
		t.Fatal(err)
	}
	if j.Status != JobCompleted || j.CompletedAt == nil || string(j.Result) != `[[1]]` {
		t.Errorf("job not completed correctly: %+v", j)
	}
}

func TestJobTerminalStatesAreFinal(t *testing.T) {
	j := newTestJob(t)
	_ = j.Cancel("user cancelled", t0)
	if !j.Status.IsTerminal() {
		t.Fatal("cancelled should be terminal")
	}
	err := j.Start(t0)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("want ErrInvalidTransition, got %v", err)
	}
	var te *TransitionError
	if !errors.As(err, &te) || te.From != "cancelled" || te.To != "running" {
		t.Errorf("unexpected error detail: %v", err)
	}
	if j.Status != JobCancelled {
		t.Error("status changed despite rejected transition")
	}
}

func TestCannotCompleteQueuedJob(t *testing.T) {
	j := newTestJob(t)
	if err := j.Complete(nil, t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("want ErrInvalidTransition, got %v", err)
	}
}

// ---------- Task ----------

func newTestTask(t *testing.T) *Task {
	t.Helper()
	return NewTask("task-1", newTestJob(t), 0, json.RawMessage(`{"rows":[0,1]}`), t0)
}

func TestNewTaskInheritsPriority(t *testing.T) {
	task := newTestTask(t)
	if task.Priority != PriorityHigh {
		t.Errorf("priority = %s, want high", task.Priority)
	}
	if task.Status != TaskPending || task.MaxAttempts != DefaultMaxAttempts {
		t.Errorf("unexpected initial task: %+v", task)
	}
}

func TestTaskTransitionTable(t *testing.T) {
	all := []TaskStatus{TaskPending, TaskAssigned, TaskRunning, TaskDone, TaskFailed}
	allowed := map[[2]TaskStatus]bool{
		{TaskPending, TaskAssigned}: true,
		{TaskAssigned, TaskRunning}: true,
		{TaskAssigned, TaskPending}: true,
		{TaskAssigned, TaskFailed}:  true,
		{TaskRunning, TaskDone}:     true,
		{TaskRunning, TaskFailed}:   true,
		{TaskRunning, TaskPending}:  true,
		{TaskFailed, TaskPending}:   true,
	}
	for _, from := range all {
		for _, to := range all {
			want := allowed[[2]TaskStatus{from, to}]
			if got := from.CanTransitionTo(to); got != want {
				t.Errorf("%s -> %s: got %v, want %v", from, to, got, want)
			}
		}
	}
}

func TestTaskHappyPath(t *testing.T) {
	task := newTestTask(t)
	must(t, task.Assign("w1", t0))
	if task.Attempts != 1 || task.WorkerID != "w1" {
		t.Errorf("after assign: %+v", task)
	}
	must(t, task.Start("w1", t0))
	must(t, task.Complete("w1", json.RawMessage(`[1,2]`), t0))
	if task.Status != TaskDone || string(task.Result) != `[1,2]` {
		t.Errorf("after complete: %+v", task)
	}
}

func TestTaskRejectsWrongWorker(t *testing.T) {
	task := newTestTask(t)
	must(t, task.Assign("w1", t0))
	if err := task.Start("w2", t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("want ErrInvalidTransition, got %v", err)
	}
}

func TestDuplicateResultRejected(t *testing.T) {
	task := newTestTask(t)
	must(t, task.Assign("w1", t0))
	must(t, task.Start("w1", t0))
	must(t, task.Complete("w1", json.RawMessage(`[1]`), t0))
	if err := task.Complete("w1", json.RawMessage(`[999]`), t0); err == nil {
		t.Fatal("duplicate result accepted")
	}
	if string(task.Result) != `[1]` {
		t.Error("result overwritten by duplicate")
	}
}

func TestLateResultFromLostWorkerRejected(t *testing.T) {
	task := newTestTask(t)
	must(t, task.Assign("w1", t0))
	must(t, task.Start("w1", t0))
	// w1 misses heartbeats; coordinator reclaims and gives task to w2.
	must(t, task.Requeue("worker w1 heartbeat timeout", t0))
	must(t, task.Assign("w2", t0))
	// w1 comes back and reports late.
	if err := task.Complete("w1", json.RawMessage(`[1]`), t0); err == nil {
		t.Fatal("late result from old worker accepted")
	}
	if task.Attempts != 2 {
		t.Errorf("attempts = %d, want 2", task.Attempts)
	}
}

func TestRetryUntilAttemptsExhausted(t *testing.T) {
	task := newTestTask(t)
	for i := 1; i <= DefaultMaxAttempts; i++ {
		must(t, task.Assign("w1", t0))
		must(t, task.Start("w1", t0))
		must(t, task.Fail("w1", "boom", t0))
		if i < DefaultMaxAttempts {
			must(t, task.Requeue("retry", t0))
		}
	}
	if !task.IsPermanentlyFailed() {
		t.Fatal("expected permanent failure")
	}
	if err := task.Requeue("again", t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("requeue after max attempts: want ErrInvalidTransition, got %v", err)
	}
}

func TestDoneTaskCannotBeRequeued(t *testing.T) {
	task := newTestTask(t)
	must(t, task.Assign("w1", t0))
	must(t, task.Start("w1", t0))
	must(t, task.Complete("w1", nil, t0))
	if err := task.Requeue("x", t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("want ErrInvalidTransition, got %v", err)
	}
}

// ---------- Progress & outcome ----------

func TestComputeProgress(t *testing.T) {
	tasks := []*Task{
		{Status: TaskDone}, {Status: TaskDone}, {Status: TaskRunning}, {Status: TaskPending},
	}
	p := ComputeProgress(tasks)
	if p.Total != 4 || p.Done != 2 || p.Running != 1 || p.Pending != 1 || p.Percent != 50 {
		t.Errorf("unexpected progress: %+v", p)
	}
	if ComputeProgress(nil).Percent != 0 {
		t.Error("empty job should be 0%")
	}
}

func TestProgressFromCounts(t *testing.T) {
	p := ProgressFromCounts(map[TaskStatus]int{TaskDone: 3, TaskPending: 1})
	if p.Total != 4 || p.Percent != 75 {
		t.Errorf("unexpected progress: %+v", p)
	}
}

func TestDecideJobOutcome(t *testing.T) {
	done := &Task{Status: TaskDone, MaxAttempts: 3}
	running := &Task{Status: TaskRunning, MaxAttempts: 3}
	retryable := &Task{Status: TaskFailed, Attempts: 1, MaxAttempts: 3}
	dead := &Task{Status: TaskFailed, Attempts: 3, MaxAttempts: 3}

	cases := []struct {
		name  string
		tasks []*Task
		want  JobOutcome
	}{
		{"no tasks", nil, OutcomeInProgress},
		{"all done", []*Task{done, done}, OutcomeCompleted},
		{"some running", []*Task{done, running}, OutcomeInProgress},
		{"retryable failure", []*Task{done, retryable}, OutcomeInProgress},
		{"permanent failure", []*Task{done, dead, running}, OutcomeFailed},
	}
	for _, c := range cases {
		if got := DecideJobOutcome(c.tasks); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// ---------- Database scanning ----------

func TestScanAndValue(t *testing.T) {
	var js JobStatus
	must(t, js.Scan([]byte("running")))
	if js != JobRunning {
		t.Errorf("got %s", js)
	}
	if err := js.Scan("bogus"); err == nil {
		t.Error("expected error for unknown job status")
	}
	var ts TaskStatus
	must(t, ts.Scan("assigned"))
	if _, err := TaskStatus("nope").Value(); err == nil {
		t.Error("expected error writing invalid task status")
	}
	var p Priority
	must(t, p.Scan("low"))
	if p.Rank() != 2 {
		t.Errorf("rank = %d", p.Rank())
	}
	if got, _ := ParsePriority(""); got != PriorityNormal {
		t.Errorf("default priority = %s", got)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
