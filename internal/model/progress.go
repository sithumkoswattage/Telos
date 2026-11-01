package model

// Progress summarises a job's tasks. It is computed on demand from task
// rows and never stored, so it cannot go stale.
type Progress struct {
	Total    int     `json:"total"`
	Pending  int     `json:"pending"`
	Assigned int     `json:"assigned"`
	Running  int     `json:"running"`
	Done     int     `json:"done"`
	Failed   int     `json:"failed"`
	Percent  float64 `json:"percent"` // done / total * 100
}

// ComputeProgress counts tasks by status. Percent is done/total*100, and 0
// for a job that has not been split yet.
func ComputeProgress(tasks []*Task) Progress {
	var p Progress
	for _, t := range tasks {
		p.Total++
		switch t.Status {
		case TaskPending:
			p.Pending++
		case TaskAssigned:
			p.Assigned++
		case TaskRunning:
			p.Running++
		case TaskDone:
			p.Done++
		case TaskFailed:
			p.Failed++
		}
	}
	if p.Total > 0 {
		p.Percent = float64(p.Done) / float64(p.Total) * 100
	}
	return p
}

// ProgressFromCounts builds Progress from a SQL aggregate such as
//
//	SELECT status, COUNT(*) FROM tasks WHERE job_id = $1 GROUP BY status
//
// so the API does not need to load every task.
func ProgressFromCounts(counts map[TaskStatus]int) Progress {
	p := Progress{
		Pending:  counts[TaskPending],
		Assigned: counts[TaskAssigned],
		Running:  counts[TaskRunning],
		Done:     counts[TaskDone],
		Failed:   counts[TaskFailed],
	}
	p.Total = p.Pending + p.Assigned + p.Running + p.Done + p.Failed
	if p.Total > 0 {
		p.Percent = float64(p.Done) / float64(p.Total) * 100
	}
	return p
}

// JobOutcome tells the coordinator what a running job should become, based
// on its tasks.
type JobOutcome int

const (
	OutcomeInProgress JobOutcome = iota // keep running
	OutcomeCompleted                    // every task is done: combine results
	OutcomeFailed                       // a task failed with no attempts left
)

// DecideJobOutcome looks at a job's tasks and reports whether the job is
// finished. A single permanently failed task fails the whole job, because a
// matrix product with a missing row block is wrong.
func DecideJobOutcome(tasks []*Task) JobOutcome {
	if len(tasks) == 0 {
		return OutcomeInProgress
	}
	allDone := true
	for _, t := range tasks {
		if t.IsPermanentlyFailed() {
			return OutcomeFailed
		}
		if t.Status != TaskDone {
			allDone = false
		}
	}
	if allDone {
		return OutcomeCompleted
	}
	return OutcomeInProgress
}
