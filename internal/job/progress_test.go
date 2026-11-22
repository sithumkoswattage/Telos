package job

import (
	"context"
	"testing"
)

// TEL-40: GET /api/jobs/{id} reports progress worked out from the tasks.
func TestGetIncludesProgress(t *testing.T) {
	database := openJobTestDB(t)
	ctx := context.Background()

	var userID, jobID string
	if err := database.QueryRow(
		`INSERT INTO users (email, password_hash) VALUES ('p@test', 'x') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(
		`INSERT INTO jobs (user_id, name, type, status) VALUES ($1, 'job', 'text_processing', 'queued') RETURNING id`,
		userID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}

	// Not split into tasks yet: nothing to count.
	d, err := Get(ctx, database, jobID, userID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Progress.Total != 0 || d.Progress.Percent != 0 {
		t.Errorf("before split: progress = %+v, want total 0 and 0%%", d.Progress)
	}

	// Four tasks: 2 done, 1 pending, 1 pending -> 50%.
	for i, status := range []string{"done", "done", "pending", "pending"} {
		if _, err := database.Exec(
			`INSERT INTO tasks (job_id, task_index, priority, status) VALUES ($1, $2, 'normal', $3)`,
			jobID, i, status); err != nil {
			t.Fatal(err)
		}
	}
	d, err = Get(ctx, database, jobID, userID)
	if err != nil {
		t.Fatal(err)
	}
	p := d.Progress
	if p.Total != 4 || p.Done != 2 || p.Pending != 2 || p.Percent != 50 {
		t.Errorf("progress = %+v, want total 4, done 2, pending 2, 50%%", p)
	}

	// Progress is never stored: finishing a task changes it straight away.
	if _, err := database.Exec(`UPDATE tasks SET status = 'done' WHERE job_id = $1 AND task_index = 2`, jobID); err != nil {
		t.Fatal(err)
	}
	d, _ = Get(ctx, database, jobID, userID)
	if d.Progress.Done != 3 || d.Progress.Percent != 75 {
		t.Errorf("after one more task: progress = %+v, want 3 done and 75%%", d.Progress)
	}
}
