package job

import (
	"context"
	"database/sql"
)

// Overview is what the admin dashboard shows: how many jobs are in each
// status, and the most recent jobs. Unlike List, it covers ALL users,
// because the admin sees everyone's jobs.
type Overview struct {
	Total    int
	ByStatus map[string]int // e.g. {"queued": 2, "completed": 5}
	Recent   []Summary      // newest first
}

// AdminOverview counts jobs by status and loads the latest recentLimit jobs.
func AdminOverview(ctx context.Context, db *sql.DB, recentLimit int) (Overview, error) {
	o := Overview{ByStatus: map[string]int{}, Recent: []Summary{}}

	// 1. One row per status, e.g. ("queued", 2), ("completed", 5).
	rows, err := db.QueryContext(ctx, `SELECT status, COUNT(*) FROM jobs GROUP BY status`)
	if err != nil {
		return Overview{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return Overview{}, err
		}
		o.ByStatus[status] = n
		o.Total += n
	}
	if err := rows.Err(); err != nil {
		return Overview{}, err
	}

	// 2. The newest jobs, from every user.
	recent, err := db.QueryContext(ctx,
		`SELECT id::text, name, type, queue, priority, status, created_at
		 FROM jobs
		 ORDER BY created_at DESC
		 LIMIT $1`, recentLimit)
	if err != nil {
		return Overview{}, err
	}
	defer recent.Close()
	for recent.Next() {
		var s Summary
		if err := recent.Scan(&s.ID, &s.Name, &s.Type, &s.Queue, &s.Priority, &s.Status, &s.CreatedAt); err != nil {
			return Overview{}, err
		}
		o.Recent = append(o.Recent, s)
	}
	return o, recent.Err()
}
