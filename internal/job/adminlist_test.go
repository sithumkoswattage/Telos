package job

import (
	"context"
	"testing"
)

// TestAdminList uses two users, one worker and four jobs:
//
//	alice: "essay"   completed, ran on worker "laptop-1"
//	alice: "notes"   queued
//	bob:   "report"  failed
//	bob:   "poem"    queued (newest)
func TestAdminList(t *testing.T) {
	database := openJobTestDB(t)
	ctx := context.Background()

	var alice, bob, worker string
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(database.QueryRow(`INSERT INTO users (email, password_hash) VALUES ('alice@test', 'x') RETURNING id`).Scan(&alice))
	must(database.QueryRow(`INSERT INTO users (email, password_hash) VALUES ('bob@test', 'x') RETURNING id`).Scan(&bob))
	must(database.QueryRow(`INSERT INTO workers (name, token_hash) VALUES ('laptop-1', '') RETURNING id`).Scan(&worker))

	jobs := []struct{ user, name, status string }{
		{alice, "essay", "completed"},
		{alice, "notes", "queued"},
		{bob, "report", "failed"},
		{bob, "poem", "queued"},
	}
	ids := map[string]string{}
	for i, j := range jobs {
		var id string
		// created_at increases by one minute per job, so "poem" is newest.
		must(database.QueryRow(
			`INSERT INTO jobs (user_id, name, type, status, created_at)
			 VALUES ($1, $2, 'text_processing', $3, now() - make_interval(mins => $4))
			 RETURNING id`,
			j.user, j.name, j.status, len(jobs)-i).Scan(&id))
		ids[j.name] = id
	}
	// "essay" ran on laptop-1.
	_, err := database.Exec(`
		INSERT INTO tasks (job_id, task_index, priority, status, worker_id, attempts)
		VALUES ($1, 0, 'normal', 'done', $2, 1)`, ids["essay"], worker)
	must(err)

	names := func(rows []AdminRow) []string {
		var out []string
		for _, r := range rows {
			out = append(out, r.Name)
		}
		return out
	}
	check := func(label string, f AdminFilter, want ...string) {
		t.Helper()
		rows, total, err := AdminList(ctx, database, f)
		must(err)
		if total != 4 {
			t.Errorf("%s: total = %d, want 4", label, total)
		}
		got := names(rows)
		if len(got) != len(want) {
			t.Fatalf("%s: got %v, want %v", label, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: got %v, want %v", label, got, want)
				break
			}
		}
	}

	check("no filter: everyone's jobs, newest first", AdminFilter{}, "poem", "report", "notes", "essay")
	check("status", AdminFilter{Status: "queued"}, "poem", "notes")
	check("search by job name", AdminFilter{Search: "REP"}, "report")
	check("search by worker name", AdminFilter{Search: "laptop"}, "essay")
	check("search by ID", AdminFilter{Search: ids["notes"][:8]}, "notes")
	check("queue", AdminFilter{Queue: "text-processing"}, "poem", "report", "notes", "essay")
	check("unknown queue", AdminFilter{Queue: "nope"})
	check("limit", AdminFilter{Limit: 2}, "poem", "report")

	rows, _, err := AdminList(ctx, database, AdminFilter{Search: "essay"})
	must(err)
	if rows[0].Worker != "laptop-1" {
		t.Errorf("essay worker = %q, want laptop-1", rows[0].Worker)
	}
	rows, _, err = AdminList(ctx, database, AdminFilter{Search: "notes"})
	must(err)
	if rows[0].Worker != "" {
		t.Errorf("notes worker = %q, want empty (no task yet)", rows[0].Worker)
	}
}
