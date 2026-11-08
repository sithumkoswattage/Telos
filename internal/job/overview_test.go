package job

import (
	"context"
	"testing"
)

func TestAdminOverviewEmptyDatabase(t *testing.T) {
	database := openJobTestDB(t)

	o, err := AdminOverview(context.Background(), database, 10)
	if err != nil {
		t.Fatal(err)
	}
	if o.Total != 0 || len(o.Recent) != 0 {
		t.Fatalf("empty database: got total %d and %d recent jobs, want 0 and 0", o.Total, len(o.Recent))
	}
}

func TestAdminOverviewCountsAllUsers(t *testing.T) {
	database := openJobTestDB(t)
	ctx := context.Background()

	// Two users, so we can check the admin sees both users' jobs.
	var alice, bob string
	if err := database.QueryRow(`INSERT INTO users (email, password_hash) VALUES ('alice@test', 'x') RETURNING id`).Scan(&alice); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`INSERT INTO users (email, password_hash) VALUES ('bob@test', 'x') RETURNING id`).Scan(&bob); err != nil {
		t.Fatal(err)
	}
	jobs := []struct{ user, name, status string }{
		{alice, "first", "completed"},
		{alice, "second", "queued"},
		{bob, "third", "queued"},
		{bob, "fourth", "failed"},
	}
	for i, j := range jobs {
		// created_at increases by one minute per job, so "fourth" is newest.
		_, err := database.Exec(
			`INSERT INTO jobs (user_id, name, type, status, created_at)
			 VALUES ($1, $2, 'text_processing', $3, now() - make_interval(mins => $4))`,
			j.user, j.name, j.status, len(jobs)-i)
		if err != nil {
			t.Fatal(err)
		}
	}

	o, err := AdminOverview(ctx, database, 3)
	if err != nil {
		t.Fatal(err)
	}
	if o.Total != 4 {
		t.Errorf("Total = %d, want 4", o.Total)
	}
	want := map[string]int{"completed": 1, "queued": 2, "failed": 1}
	for status, n := range want {
		if o.ByStatus[status] != n {
			t.Errorf("ByStatus[%q] = %d, want %d", status, o.ByStatus[status], n)
		}
	}
	if o.ByStatus["running"] != 0 {
		t.Errorf("ByStatus[running] = %d, want 0", o.ByStatus["running"])
	}
	// Limit is 3, newest first.
	if len(o.Recent) != 3 {
		t.Fatalf("got %d recent jobs, want 3", len(o.Recent))
	}
	for i, name := range []string{"fourth", "third", "second"} {
		if o.Recent[i].Name != name {
			t.Errorf("Recent[%d] = %q, want %q", i, o.Recent[i].Name, name)
		}
	}
}
