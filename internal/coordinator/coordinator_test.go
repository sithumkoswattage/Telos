package coordinator

// Database tests. They use the telos_test database (see the README) and are
// skipped without TELOS_TEST_DATABASE_URL. Run them with: go test -p 1 ./...

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"telos/internal/db"
	"telos/internal/model"
)

type fixture struct {
	db     *sql.DB
	c      *Coordinator
	worker string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("TELOS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TELOS_TEST_DATABASE_URL not set; skipping database tests")
	}
	database, err := sql.Open("pgx", dsn)
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
	f := &fixture{db: database, c: New(database)}
	if err := database.QueryRow(
		`INSERT INTO workers (name, token_hash) VALUES ('w1', '') RETURNING id`).Scan(&f.worker); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) submit(t *testing.T, content string) string {
	t.Helper()
	var user, id string
	err := f.db.QueryRow(`INSERT INTO users (email, password_hash) VALUES (gen_random_uuid()::text, 'x') RETURNING id`).Scan(&user)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(map[string]string{"content": content})
	err = f.db.QueryRow(`INSERT INTO jobs (user_id, name, type, input) VALUES ($1, 'test', 'text_processing', $2) RETURNING id`,
		user, input).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *fixture) job(t *testing.T, id string) (status string, result []byte, reason string) {
	t.Helper()
	var res sql.NullString
	err := f.db.QueryRow(`SELECT status, result, error FROM jobs WHERE id = $1`, id).Scan(&status, &res, &reason)
	if err != nil {
		t.Fatal(err)
	}
	return status, []byte(res.String), reason
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestQueuedJobBecomesOneTask(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	id := f.submit(t, "hello world")

	must(t, f.c.Tick(ctx))

	if s, _, _ := f.job(t, id); s != "running" {
		t.Fatalf("job status = %s, want running", s)
	}
	tasks, err := f.c.Queue.Tasks(ctx, id)
	must(t, err)
	if len(tasks) != 1 || tasks[0].Status != model.TaskPending {
		t.Fatalf("want 1 pending task, got %+v", tasks)
	}
	var in map[string]string
	must(t, json.Unmarshal(tasks[0].Input, &in))
	if in["content"] != "hello world" {
		t.Errorf("task input = %s", tasks[0].Input)
	}

	// A second tick must not create a second task.
	must(t, f.c.Tick(ctx))
	tasks, _ = f.c.Queue.Tasks(ctx, id)
	if len(tasks) != 1 {
		t.Errorf("second tick created more tasks: %d", len(tasks))
	}
}

func TestDoneTaskCompletesJobWithResult(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	id := f.submit(t, "one two three")
	must(t, f.c.Tick(ctx))

	task, err := f.c.Queue.ClaimTask(ctx, f.worker)
	must(t, err)
	// Still running while the worker is busy.
	must(t, f.c.Tick(ctx))
	if s, _, _ := f.job(t, id); s != "running" {
		t.Fatalf("job status = %s, want running", s)
	}

	must(t, f.c.Queue.CompleteTask(ctx, task.ID, f.worker, json.RawMessage(`{"words":3}`)))
	must(t, f.c.Tick(ctx))

	status, result, _ := f.job(t, id)
	if status != "completed" {
		t.Fatalf("job status = %s, want completed", status)
	}
	var r map[string]int
	must(t, json.Unmarshal(result, &r))
	if r["words"] != 3 {
		t.Errorf("job result = %s", result)
	}
}

func TestPermanentTaskFailureFailsJob(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	id := f.submit(t, "text")
	must(t, f.c.Tick(ctx))

	for i := 0; i < model.DefaultMaxAttempts; i++ {
		task, err := f.c.Queue.ClaimTask(ctx, f.worker)
		must(t, err)
		_, err = f.c.Queue.FailTask(ctx, task.ID, f.worker, "boom")
		must(t, err)
		must(t, f.c.Tick(ctx))
	}

	status, _, reason := f.job(t, id)
	if status != "failed" || reason == "" {
		t.Fatalf("job = %s (%q), want failed with a reason", status, reason)
	}
}
