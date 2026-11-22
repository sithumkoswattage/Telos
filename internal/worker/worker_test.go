package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeServer pretends to be the Telos server. It hands out the given tasks
// one by one and records what the worker sends back.
type fakeServer struct {
	mu        sync.Mutex
	tasks     []Task
	calls     []string                   // e.g. "start t1", "complete t1"
	bodies    map[string]json.RawMessage // last body per call
	failFirst map[string]int             // path -> how many 500s to send first
	status    map[string]int             // path -> fixed status to reply with
	hits      map[string]int             // path -> number of requests
}

func newFakeServer(tasks ...Task) *fakeServer {
	return &fakeServer{
		tasks:     tasks,
		bodies:    map[string]json.RawMessage{},
		failFirst: map[string]int{},
		status:    map[string]int{},
		hits:      map[string]int{},
	}
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var body json.RawMessage
	json.NewDecoder(r.Body).Decode(&body)

	path := r.URL.Path
	f.hits[path]++
	if f.failFirst[path] > 0 {
		f.failFirst[path]--
		http.Error(w, `{"error":"try later"}`, http.StatusInternalServerError)
		return
	}
	if code := f.status[path]; code != 0 {
		w.WriteHeader(code)
		w.Write([]byte(`{"error":"not yours"}`))
		return
	}

	switch {
	case path == "/api/workers/register":
		w.Write([]byte(`{"id":"w-1"}`))
	case path == "/api/worker/tasks/claim":
		if len(f.tasks) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t := f.tasks[0]
		f.tasks = f.tasks[1:]
		json.NewEncoder(w).Encode(t)
	default: // /api/worker/tasks/{id}/{action}
		parts := strings.Split(strings.TrimPrefix(path, "/api/worker/tasks/"), "/")
		call := parts[1] + " " + parts[0]
		f.calls = append(f.calls, call)
		f.bodies[call] = body
		w.WriteHeader(http.StatusNoContent)
	}
}

func (f *fakeServer) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeServer) body(call string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var m map[string]any
	json.Unmarshal(f.bodies[call], &m)
	return m
}

// testWorker returns a fast worker that is already registered.
func testWorker(t *testing.T, f *fakeServer) *Worker {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	w := New(srv.URL, "test")
	w.ID = "w-1"
	w.PollInterval = 10 * time.Millisecond
	w.RetryDelay = time.Millisecond
	w.Logf = t.Logf
	return w
}

func equal(a, b []string) bool {
	return strings.Join(a, ",") == strings.Join(b, ",")
}

func TestProcessComputesAndReportsResult(t *testing.T) {
	f := newFakeServer()
	w := testWorker(t, f)

	w.Process(Task{ID: "t1", JobID: "j1", Input: json.RawMessage(`{"content":"one two three"}`)})

	if want := []string{"start t1", "complete t1"}; !equal(f.seen(), want) {
		t.Fatalf("calls = %v, want %v", f.seen(), want)
	}
	body := f.body("complete t1")
	if body["worker_id"] != "w-1" {
		t.Errorf("worker_id = %v, want w-1", body["worker_id"])
	}
	result, _ := body["result"].(map[string]any)
	if result["words"] != float64(3) {
		t.Errorf("result = %v, want 3 words", body["result"])
	}
}

func TestBadInputIsReportedAsFailure(t *testing.T) {
	f := newFakeServer()
	w := testWorker(t, f)

	w.Process(Task{ID: "t1", Input: json.RawMessage(`{"content":"   "}`)})

	if want := []string{"start t1", "fail t1"}; !equal(f.seen(), want) {
		t.Fatalf("calls = %v, want %v", f.seen(), want)
	}
	if f.body("fail t1")["error"] == "" {
		t.Error("failure was reported without a reason")
	}
}

func TestCrashIsReportedAsFailure(t *testing.T) {
	f := newFakeServer()
	w := testWorker(t, f)
	w.Compute = func(json.RawMessage) (json.RawMessage, error) { panic("boom") }

	w.Process(Task{ID: "t1"}) // must not crash the test

	if want := []string{"start t1", "fail t1"}; !equal(f.seen(), want) {
		t.Fatalf("calls = %v, want %v", f.seen(), want)
	}
}

func TestResultIsResentWhenServerHasTrouble(t *testing.T) {
	f := newFakeServer()
	f.failFirst["/api/worker/tasks/t1/complete"] = 2 // two 500s, then OK
	w := testWorker(t, f)

	w.Process(Task{ID: "t1", Input: json.RawMessage(`{"content":"hi"}`)})

	if want := []string{"start t1", "complete t1"}; !equal(f.seen(), want) {
		t.Errorf("calls = %v, want %v (the result should get through)", f.seen(), want)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if n := f.hits["/api/worker/tasks/t1/complete"]; n != 3 {
		t.Errorf("sent the result %d times, want 3 (2 failures + 1 success)", n)
	}
}

func TestNoRetryWhenTaskIsNoLongerOurs(t *testing.T) {
	f := newFakeServer()
	f.status["/api/worker/tasks/t1/complete"] = http.StatusConflict
	w := testWorker(t, f)

	if w.report(context.Background(), "t1", "complete", map[string]any{"result": 1}) {
		t.Error("report should return false on 409")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if n := f.hits["/api/worker/tasks/t1/complete"]; n != 1 {
		t.Errorf("sent the result %d times, want 1 (no retries)", n)
	}
}

func TestClaimWithNoWork(t *testing.T) {
	w := testWorker(t, newFakeServer())
	if _, err := w.Claim(context.Background()); err != ErrNoTask {
		t.Errorf("Claim = %v, want ErrNoTask", err)
	}
}

func TestRunDoesAllTasksThenStopsOnCancel(t *testing.T) {
	f := newFakeServer(
		Task{ID: "t1", Index: 0, Input: json.RawMessage(`{"content":"a b"}`)},
		Task{ID: "t2", Index: 1, Input: json.RawMessage(`{"content":"c d e"}`)},
	)
	w := testWorker(t, f)
	w.ID = "" // let Run register

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	// Wait until both tasks are reported, then press "Ctrl+C".
	deadline := time.Now().Add(2 * time.Second)
	for len(f.seen()) < 4 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after cancel")
	}
	want := []string{"start t1", "complete t1", "start t2", "complete t2"}
	if !equal(f.seen(), want) {
		t.Errorf("calls = %v, want %v", f.seen(), want)
	}
	if w.ID != "w-1" {
		t.Errorf("worker ID = %q, want w-1 from register", w.ID)
	}
}
