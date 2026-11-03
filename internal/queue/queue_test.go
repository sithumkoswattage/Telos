package queue

// Integration tests: they need a real, EMPTY PostgreSQL database because they
// delete all rows before each test. Do NOT point them at your main `telos`
// database. Create a separate one once:
//
//	psql -U postgres -c "CREATE DATABASE telos_test;"
//
// then run (PowerShell):
//
//	$env:TELOS_TEST_DATABASE_URL="postgres://postgres:YOURPASSWORD@localhost:5432/telos_test?sslmode=disable"
//	go test ./internal/queue/ -v
//
// Without TELOS_TEST_DATABASE_URL the tests are skipped.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"telos/internal/db"
	"telos/internal/model"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TELOS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TELOS_TEST_DATABASE_URL not set; skipping database tests")
	}
	database, err := sql.Open(testDriver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`TRUNCATE events, tasks, jobs, workers, users CASCADE`); err != nil {
		t.Fatal(err)
	}
	return database
}

// testDriver is the database/sql driver name used by the tests.
var testDriver = "pgx"

type fixture struct {
	db     *sql.DB
	q      *Queue
	userID string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	database := openTestDB(t)
	f := &fixture{db: database, q: New(database)}
	err := database.QueryRow(
		`INSERT INTO users (email, password_hash) VALUES ('test@telos.local', 'x') RETURNING id`,
	).Scan(&f.userID)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) job(t *testing.T, p model.Priority) string {
	t.Helper()
	var id string
	err := f.db.QueryRow(
		`INSERT INTO jobs (user_id, type, priority) VALUES ($1, 'matrix_multiply', $2) RETURNING id`,
		f.userID, p).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *fixture) worker(t *testing.T, name string) string {
	t.Helper()
	var id string
	err := f.db.QueryRow(
		`INSERT INTO workers (name, token_hash) VALUES ($1, 'x') RETURNING id`, name).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func inputs(n int) []json.RawMessage {
	out := make([]json.RawMessage, n)
	for i := range out {
		out[i] = json.RawMessage(fmt.Sprintf(`{"rows":[%d]}`, i))
	}
	return out
}

func (f *fixture) jobStatus(t *testing.T, id string) model.JobStatus {
	t.Helper()
	var s model.JobStatus
	if err := f.db.QueryRow(`SELECT status FROM jobs WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------- tests

func TestListQueuedJobsOrder(t *testing.T) {
	f := setup(t)
	low := f.job(t, model.PriorityLow)
	high1 := f.job(t, model.PriorityHigh)
	normal := f.job(t, model.PriorityNormal)
	high2 := f.job(t, model.PriorityHigh)

	jobs, err := f.q.ListQueuedJobs(context.Background(), 10)
	must(t, err)
	want := []string{high1, high2, normal, low}
	if len(jobs) != len(want) {
		t.Fatalf("got %d jobs, want %d", len(jobs), len(want))
	}
	for i, j := range jobs {
		if j.ID != want[i] {
			t.Errorf("position %d: got %s, want %s", i, j.ID, want[i])
		}
	}
}

func TestEnqueueTasksStartsJob(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	jobID := f.job(t, model.PriorityHigh)

	tasks, err := f.q.EnqueueTasks(ctx, jobID, inputs(3))
	must(t, err)
	if len(tasks) != 3 {
		t.Fatalf("got %d tasks", len(tasks))
	}
	for i, task := range tasks {
		if task.Index != i || task.Status != model.TaskPending || task.Priority != model.PriorityHigh {
			t.Errorf("task %d: %+v", i, task)
		}
	}
	if s := f.jobStatus(t, jobID); s != model.JobRunning {
		t.Errorf("job status = %s, want running", s)
	}
	queued, _ := f.q.ListQueuedJobs(ctx, 10)
	if len(queued) != 0 {
		t.Error("running job still listed as queued")
	}
}

func TestEnqueueTasksTwiceIsRejected(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	jobID := f.job(t, model.PriorityNormal)
	_, err := f.q.EnqueueTasks(ctx, jobID, inputs(2))
	must(t, err)

	_, err = f.q.EnqueueTasks(ctx, jobID, inputs(2))
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
	all, _ := f.q.Tasks(ctx, jobID)
	if len(all) != 2 {
		t.Errorf("job has %d tasks, want 2 (second split must write nothing)", len(all))
	}
}

func TestEnqueueTasksUnknownJob(t *testing.T) {
	f := setup(t)
	_, err := f.q.EnqueueTasks(context.Background(), "00000000-0000-0000-0000-000000000000", inputs(1))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestClaimOrderAndEmpty(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	w := f.worker(t, "w1")
	lowJob := f.job(t, model.PriorityLow)
	highJob := f.job(t, model.PriorityHigh)
	_, err := f.q.EnqueueTasks(ctx, lowJob, inputs(1))
	must(t, err)
	_, err = f.q.EnqueueTasks(ctx, highJob, inputs(2))
	must(t, err)

	var got []string
	for range 3 {
		task, err := f.q.ClaimTask(ctx, w)
		must(t, err)
		if task.Status != model.TaskAssigned || task.WorkerID != w || task.Attempts != 1 {
			t.Errorf("claimed task looks wrong: %+v", task)
		}
		got = append(got, fmt.Sprintf("%s/%d", task.JobID, task.Index))
	}
	want := []string{highJob + "/0", highJob + "/1", lowJob + "/0"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("claim %d: got %s, want %s", i, got[i], want[i])
		}
	}
	if _, err := f.q.ClaimTask(ctx, w); !errors.Is(err, ErrEmpty) {
		t.Fatalf("want ErrEmpty, got %v", err)
	}
}

func TestClaimTaskForJob(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	w := f.worker(t, "w1")
	a := f.job(t, model.PriorityHigh)
	b := f.job(t, model.PriorityLow)
	_, _ = f.q.EnqueueTasks(ctx, a, inputs(1))
	_, _ = f.q.EnqueueTasks(ctx, b, inputs(1))

	task, err := f.q.ClaimTaskForJob(ctx, w, b)
	must(t, err)
	if task.JobID != b {
		t.Errorf("got task of job %s, want %s", task.JobID, b)
	}
}

func TestCancelledJobTasksAreNotClaimed(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	w := f.worker(t, "w1")
	jobID := f.job(t, model.PriorityHigh)
	_, err := f.q.EnqueueTasks(ctx, jobID, inputs(2))
	must(t, err)
	_, err = f.db.Exec(`UPDATE jobs SET status = 'cancelled' WHERE id = $1`, jobID)
	must(t, err)

	if _, err := f.q.ClaimTask(ctx, w); !errors.Is(err, ErrEmpty) {
		t.Fatalf("want ErrEmpty for cancelled job, got %v", err)
	}
}

func TestTaskLifecycle(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	w := f.worker(t, "w1")
	jobID := f.job(t, model.PriorityNormal)
	_, _ = f.q.EnqueueTasks(ctx, jobID, inputs(1))

	task, err := f.q.ClaimTask(ctx, w)
	must(t, err)
	must(t, f.q.StartTask(ctx, task.ID, w))
	must(t, f.q.CompleteTask(ctx, task.ID, w, json.RawMessage(`[[1,2]]`)))

	tasks, err := f.q.Tasks(ctx, jobID)
	must(t, err)
	got := tasks[0]
	if got.Status != model.TaskDone || got.CompletedAt == nil {
		t.Errorf("task not done: %+v", got)
	}
	var res [][]int
	must(t, json.Unmarshal(got.Result, &res))
	if len(res) != 1 || res[0][1] != 2 {
		t.Errorf("result = %s", got.Result)
	}
	if model.DecideJobOutcome(tasks) != model.OutcomeCompleted {
		t.Error("job outcome should be completed")
	}
}

func TestDuplicateAndForeignResultsRejected(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	w1, w2 := f.worker(t, "w1"), f.worker(t, "w2")
	jobID := f.job(t, model.PriorityNormal)
	_, _ = f.q.EnqueueTasks(ctx, jobID, inputs(1))
	task, _ := f.q.ClaimTask(ctx, w1)

	if err := f.q.CompleteTask(ctx, task.ID, w2, json.RawMessage(`1`)); !errors.Is(err, ErrConflict) {
		t.Errorf("other worker's result: want ErrConflict, got %v", err)
	}
	must(t, f.q.CompleteTask(ctx, task.ID, w1, json.RawMessage(`1`)))
	if err := f.q.CompleteTask(ctx, task.ID, w1, json.RawMessage(`999`)); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate result: want ErrConflict, got %v", err)
	}
	if e := f.q.StartTask(ctx, "00000000-0000-0000-0000-000000000000", w1); !errors.Is(e, ErrNotFound) {
		t.Errorf("unknown task: want ErrNotFound, got %v", e)
	}
	tasks, _ := f.q.Tasks(ctx, jobID)
	if string(tasks[0].Result) != "1" {
		t.Errorf("result overwritten: %s", tasks[0].Result)
	}
}

func TestFailRetriesThenGivesUp(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	w := f.worker(t, "w1")
	jobID := f.job(t, model.PriorityNormal)
	_, _ = f.q.EnqueueTasks(ctx, jobID, inputs(1))

	for attempt := 1; attempt <= model.DefaultMaxAttempts; attempt++ {
		task, e := f.q.ClaimTask(ctx, w)
		must(t, e)
		if task.Attempts != attempt {
			t.Fatalf("attempt %d: attempts = %d", attempt, task.Attempts)
		}
		retried, e := f.q.FailTask(ctx, task.ID, w, "boom")
		must(t, e)
		if wantRetry := attempt < model.DefaultMaxAttempts; retried != wantRetry {
			t.Fatalf("attempt %d: retried = %v, want %v", attempt, retried, wantRetry)
		}
	}
	if _, e := f.q.ClaimTask(ctx, w); !errors.Is(e, ErrEmpty) {
		t.Fatalf("permanently failed task was handed out again: %v", e)
	}
	tasks, _ := f.q.Tasks(ctx, jobID)
	if !tasks[0].IsPermanentlyFailed() || model.DecideJobOutcome(tasks) != model.OutcomeFailed {
		t.Errorf("task should be permanently failed: %+v", tasks[0])
	}
}

func TestRequeueLostWorker(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	lost, healthy := f.worker(t, "lost"), f.worker(t, "healthy")
	jobID := f.job(t, model.PriorityNormal)
	_, _ = f.q.EnqueueTasks(ctx, jobID, inputs(3))

	t1, _ := f.q.ClaimTask(ctx, lost)
	t2, _ := f.q.ClaimTask(ctx, lost)
	must(t, f.q.StartTask(ctx, t2.ID, lost))
	_, _ = f.q.ClaimTask(ctx, healthy)

	requeued, failed, e := f.q.RequeueWorkerTasks(ctx, lost, "heartbeat timeout")
	must(t, e)
	if requeued != 2 || failed != 0 {
		t.Fatalf("requeued=%d failed=%d, want 2 and 0", requeued, failed)
	}
	// The lost worker's late result must be rejected.
	if e := f.q.CompleteTask(ctx, t1.ID, lost, json.RawMessage(`1`)); !errors.Is(e, ErrConflict) {
		t.Errorf("late result from lost worker: want ErrConflict, got %v", e)
	}
	// The healthy worker picks the reclaimed tasks up.
	for range 2 {
		task, e := f.q.ClaimTask(ctx, healthy)
		must(t, e)
		if task.Attempts != 2 {
			t.Errorf("reclaimed task attempts = %d, want 2", task.Attempts)
		}
	}
	p, e := f.q.Progress(ctx, jobID)
	must(t, e)
	if p.Total != 3 || p.Assigned != 3 {
		t.Errorf("progress = %+v", p)
	}
}

// Many workers claiming at once must never get the same task twice.
func TestConcurrentClaimsAreExclusive(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	const nTasks, nWorkers = 50, 8
	jobID := f.job(t, model.PriorityNormal)
	_, e := f.q.EnqueueTasks(ctx, jobID, inputs(nTasks))
	must(t, e)

	workers := make([]string, nWorkers)
	for i := range workers {
		workers[i] = f.worker(t, fmt.Sprintf("w%d", i))
	}

	var mu sync.Mutex
	seen := map[string]string{}
	var wg sync.WaitGroup
	for _, w := range workers {
		wg.Add(1)
		go func(w string) {
			defer wg.Done()
			for {
				task, e := f.q.ClaimTask(ctx, w)
				if errors.Is(e, ErrEmpty) {
					return
				}
				if e != nil {
					t.Error(e)
					return
				}
				mu.Lock()
				if other, dup := seen[task.ID]; dup {
					t.Errorf("task %s claimed by both %s and %s", task.ID, other, w)
				}
				seen[task.ID] = w
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()
	if len(seen) != nTasks {
		t.Errorf("claimed %d distinct tasks, want %d", len(seen), nTasks)
	}
}

// The queue must survive a restart: state written through one connection
// pool is still there through a brand-new one.
func TestQueueSurvivesRestart(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	w := f.worker(t, "w1")
	jobID := f.job(t, model.PriorityNormal)
	_, _ = f.q.EnqueueTasks(ctx, jobID, inputs(2))
	first, _ := f.q.ClaimTask(ctx, w)
	f.db.Close() // "coordinator crashes"

	fresh, e := sql.Open(testDriver, os.Getenv("TELOS_TEST_DATABASE_URL"))
	must(t, e)
	defer fresh.Close()
	q := New(fresh)

	tasks, e := q.Tasks(ctx, jobID)
	must(t, e)
	if len(tasks) != 2 || tasks[0].ID != first.ID || tasks[0].Status != model.TaskAssigned {
		t.Fatalf("state lost after restart: %+v", tasks)
	}
	second, e := q.ClaimTask(ctx, w)
	must(t, e)
	if second.ID == first.ID {
		t.Error("already-claimed task handed out again after restart")
	}
}
