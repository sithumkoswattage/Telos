// Package worker is a Telos worker node (TEL-38).
//
// A worker is like a cashier at a bank counter. It keeps calling "next!"
// (claim a task), serves that customer (compute), and writes down what it
// did (report the result). When nobody is waiting it pauses a moment and
// calls again. Many workers can run at once; each takes different tasks, so
// one job's chunks are processed in parallel.
//
// The worker only talks to the server over HTTP (see internal/api/worker.go)
// and never touches the database.
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"telos/internal/textjob"
)

// ErrNoTask means the server had no task for us right now (HTTP 204).
var ErrNoTask = errors.New("no task available")

// StatusError is a reply from the server with an error status code.
type StatusError struct {
	Code    int
	Message string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("server replied %d %s: %s", e.Code, http.StatusText(e.Code), e.Message)
}

// Task is what the server hands out when we claim work.
type Task struct {
	ID       string          `json:"id"`
	JobID    string          `json:"job_id"`
	Index    int             `json:"index"` // which chunk of the job this is
	Attempts int             `json:"attempts"`
	Input    json.RawMessage `json:"input"`
}

// Worker pulls tasks from the server, computes them and reports the results.
// Use New, then Run.
type Worker struct {
	Server string // e.g. http://localhost:8080
	Name   string // unique per worker, e.g. "laptop-1"
	ID     string // set by Register

	Client *http.Client
	// Compute does the actual work for one task. New sets it to textjob.Run.
	Compute func(input json.RawMessage) (json.RawMessage, error)

	PollInterval   time.Duration // wait this long when there is no work
	RetryDelay     time.Duration // first wait before re-sending a report; doubles each time
	ReportAttempts int           // how many times to try sending a report
	Logf           func(format string, args ...any)
}

// New returns a worker for the given server with sensible defaults.
func New(server, name string) *Worker {
	return &Worker{
		Server:         server,
		Name:           name,
		Client:         &http.Client{Timeout: 10 * time.Second},
		Compute:        textjob.Run,
		PollInterval:   time.Second,
		RetryDelay:     time.Second,
		ReportAttempts: 5,
		Logf:           log.Printf,
	}
}

// Run registers with the server and then works until ctx is cancelled
// (for example by Ctrl+C). A task that is in progress when ctx is cancelled
// is still finished and reported, so stopping a worker never leaves a task
// half done.
func (w *Worker) Run(ctx context.Context) error {
	// 1. Register. Keep trying, so a worker can be started before the server.
	for {
		err := w.Register(ctx)
		if err == nil {
			break
		}
		w.Logf("Could not register (%v); retrying in 3s", err)
		if !sleep(ctx, 3*time.Second) {
			return ctx.Err()
		}
	}
	w.Logf("Worker %q registered with ID %s", w.Name, w.ID)

	// 2. Work loop: claim -> compute -> report, until told to stop.
	for {
		if ctx.Err() != nil {
			w.Logf("Worker %q stopping", w.Name)
			return nil
		}
		t, err := w.Claim(ctx)
		switch {
		case errors.Is(err, ErrNoTask):
			sleep(ctx, w.PollInterval)
		case ctx.Err() != nil:
			// We were stopped while asking; the loop check above exits.
		case err != nil:
			w.Logf("Could not get a task: %v", err)
			sleep(ctx, 3*time.Second)
		default:
			w.Process(t)
		}
	}
}

// Register tells the server about this worker and stores the ID it gets.
// Registering again with the same name gives the same ID.
func (w *Worker) Register(ctx context.Context) error {
	var resp struct {
		ID string `json:"id"`
	}
	if err := w.post(ctx, "/api/workers/register", map[string]any{"name": w.Name}, &resp); err != nil {
		return err
	}
	if resp.ID == "" {
		return errors.New("server sent no worker ID")
	}
	w.ID = resp.ID
	return nil
}

// Claim asks the server for the next task. It returns ErrNoTask when there
// is nothing to do.
func (w *Worker) Claim(ctx context.Context) (Task, error) {
	var t Task
	err := w.post(ctx, "/api/worker/tasks/claim", nil, &t)
	return t, err
}

// Process runs one task and reports the outcome. It deliberately does not
// take the Run context: once we have a task, we finish it.
func (w *Worker) Process(t Task) {
	ctx := context.Background()
	w.Logf("Task %s (job %s, chunk %d, attempt %d): started", t.ID, t.JobID, t.Index, t.Attempts)

	if err := w.post(ctx, taskPath(t.ID, "start"), nil, nil); err != nil {
		// The server no longer thinks this task is ours (or is down).
		// Without "start" we must not report a result, so we give it up.
		w.Logf("Task %s: could not mark as started: %v", t.ID, err)
		return
	}

	result, err := w.compute(t.Input)
	if err != nil {
		w.Logf("Task %s: failed: %v", t.ID, err)
		w.report(ctx, t.ID, "fail", map[string]any{"error": err.Error()})
		return
	}
	if w.report(ctx, t.ID, "complete", map[string]any{"result": result}) {
		w.Logf("Task %s: done, result %s", t.ID, result)
	}
}

// compute calls Compute, turning a crash (panic) into an ordinary error so
// one bad task cannot kill the whole worker.
func (w *Worker) compute(input json.RawMessage) (result json.RawMessage, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("task crashed: %v", r)
		}
	}()
	return w.Compute(input)
}

// report sends a task's outcome ("complete" or "fail"). The work is already
// done, so if the server cannot be reached we try again, waiting a little
// longer each time (1s, 2s, 4s...). If the server answers with a 4xx error
// (for example 409: the task was given to someone else) retrying cannot
// help, so we stop. It returns true if the server accepted the report.
func (w *Worker) report(ctx context.Context, taskID, action string, body map[string]any) bool {
	delay := w.RetryDelay
	for attempt := 1; ; attempt++ {
		err := w.post(ctx, taskPath(taskID, action), body, nil)
		if err == nil {
			return true
		}
		var se *StatusError
		if errors.As(err, &se) && se.Code < 500 {
			w.Logf("Task %s: server refused %s: %v", taskID, action, err)
			return false
		}
		if attempt >= w.ReportAttempts {
			w.Logf("Task %s: giving up on %s after %d tries: %v", taskID, action, attempt, err)
			return false
		}
		w.Logf("Task %s: could not send %s (%v); retrying in %s", taskID, action, err, delay)
		time.Sleep(delay)
		delay *= 2
	}
}

func taskPath(taskID, action string) string {
	return "/api/worker/tasks/" + taskID + "/" + action
}

// post sends body (plus our worker_id) as JSON to the server. If out is not
// nil, the JSON reply is decoded into it, and HTTP 204 returns ErrNoTask.
func (w *Worker) post(ctx context.Context, path string, body map[string]any, out any) error {
	payload := map[string]any{}
	for k, v := range body {
		payload[k] = v
	}
	if w.ID != "" {
		payload["worker_id"] = w.ID
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.Server+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNoContent:
		if out != nil {
			return ErrNoTask
		}
		return nil
	case resp.StatusCode >= 300:
		var e struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		return &StatusError{Code: resp.StatusCode, Message: e.Error}
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// sleep waits for d, or less if ctx is cancelled. It reports whether the
// full time passed.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
