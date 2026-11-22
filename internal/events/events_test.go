package events

// Database tests. They use the telos_test database and are skipped without
// TELOS_TEST_DATABASE_URL. Run them with: go test -p 1 ./...

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"telos/internal/db"
)

func openEventsTestDB(t *testing.T) *sql.DB {
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
	return database
}

func TestRecordAndReadTimeline(t *testing.T) {
	database := openEventsTestDB(t)
	ctx := context.Background()

	var userID, jobID, otherJobID, taskID, workerID string
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(database.QueryRow(`INSERT INTO users (email, password_hash) VALUES ('e@test', 'x') RETURNING id`).Scan(&userID))
	must(database.QueryRow(`INSERT INTO jobs (user_id, name, type) VALUES ($1, 'a', 'text_processing') RETURNING id`, userID).Scan(&jobID))
	must(database.QueryRow(`INSERT INTO jobs (user_id, name, type) VALUES ($1, 'b', 'text_processing') RETURNING id`, userID).Scan(&otherJobID))
	must(database.QueryRow(`INSERT INTO tasks (job_id, task_index, priority) VALUES ($1, 2, 'normal') RETURNING id`, jobID).Scan(&taskID))
	must(database.QueryRow(`INSERT INTO workers (name, token_hash) VALUES ('laptop-1', '') RETURNING id`).Scan(&workerID))

	Job(ctx, database, jobID, "", JobSubmitted, "Job submitted")
	Task(ctx, database, taskID, workerID, TaskAssigned, "Task assigned") // job looked up from the task
	Job(ctx, database, otherJobID, "", JobSubmitted, "other job")        // must not show up below
	Worker(ctx, database, workerID, WorkerRegistered, "Worker registered")
	// An unknown task writes nothing (and does not crash).
	Task(ctx, database, "00000000-0000-0000-0000-000000000000", "", TaskStarted, "nothing")

	list, err := ForJob(ctx, database, jobID)
	must(err)
	if len(list) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(list), list)
	}
	if list[0].Type != JobSubmitted || list[0].Task != nil || list[0].Worker != "" {
		t.Errorf("first event = %+v", list[0])
	}
	second := list[1]
	if second.Type != TaskAssigned || second.Task == nil || *second.Task != 2 || second.Worker != "laptop-1" {
		t.Errorf("second event = %+v, want task.assigned for task 2 by laptop-1", second)
	}

	var workerEvents int
	must(database.QueryRow(`SELECT COUNT(*) FROM events WHERE type = $1 AND job_id IS NULL`, WorkerRegistered).Scan(&workerEvents))
	if workerEvents != 1 {
		t.Errorf("worker.registered events without a job = %d, want 1", workerEvents)
	}
}

func TestForJobWithNoEventsIsEmpty(t *testing.T) {
	database := openEventsTestDB(t)
	list, err := ForJob(context.Background(), database, "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if list == nil || len(list) != 0 {
		t.Errorf("want an empty (not nil) list, got %#v", list)
	}
}
