package job

import (
	"context"
	"database/sql"
	"time"
)

// AdminFilter is what the admin typed or picked on the Jobs List page.
// An empty field means "don't filter on this".
type AdminFilter struct {
	Search string // matches job ID, job name or worker name
	Status string // e.g. "completed"
	Queue  string // e.g. "text-processing"
	Limit  int    // how many jobs to show at most
}

// AdminRow is one job on the admin Jobs List.
type AdminRow struct {
	Summary
	Worker      string     // worker name(s) that ran its tasks, "" if none yet
	StartedAt   *time.Time // when the coordinator started it
	CompletedAt *time.Time // when it finished (completed or failed)
}

// AdminList returns jobs from ALL users, newest first, matching the filter.
// It also returns how many jobs exist in total (ignoring the filter).
func AdminList(ctx context.Context, db *sql.DB, f AdminFilter) ([]AdminRow, int, error) {
	if f.Limit <= 0 {
		f.Limit = 100
	}

	// LEFT JOIN: jobs that have no tasks or no worker yet are still listed.
	// string_agg joins the names of the workers that ran the job's tasks.
	rows, err := db.QueryContext(ctx, `
		SELECT j.id::text, j.name, j.type, j.queue, j.priority, j.status,
		       j.created_at, j.started_at, j.completed_at,
		       COALESCE(string_agg(DISTINCT w.name, ', '), '') AS workers
		FROM jobs j
		LEFT JOIN tasks t   ON t.job_id = j.id
		LEFT JOIN workers w ON w.id = t.worker_id
		WHERE ($1 = '' OR j.status = $1)
		  AND ($2 = '' OR j.queue = $2)
		  AND ($3 = '' OR j.id::text ILIKE '%' || $3 || '%'
		               OR j.name     ILIKE '%' || $3 || '%'
		               OR w.name     ILIKE '%' || $3 || '%')
		GROUP BY j.id
		ORDER BY j.created_at DESC
		LIMIT $4`,
		f.Status, f.Queue, f.Search, f.Limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := []AdminRow{}
	for rows.Next() {
		var r AdminRow
		var started, completed sql.NullTime
		err := rows.Scan(&r.ID, &r.Name, &r.Type, &r.Queue, &r.Priority, &r.Status,
			&r.CreatedAt, &started, &completed, &r.Worker)
		if err != nil {
			return nil, 0, err
		}
		if started.Valid {
			r.StartedAt = &started.Time
		}
		if completed.Valid {
			r.CompletedAt = &completed.Time
		}
		list = append(list, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var total int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs`).Scan(&total); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}
