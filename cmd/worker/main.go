// Command worker is a Telos worker node (TEL-80).
//
// It registers with the server, then loops forever:
//
//	ask for a task -> count the words -> send the result back
//
// When there is no work it waits a second and asks again.
//
// Run it (with the server already running) in a second terminal:
//
//	go run ./cmd/worker -name laptop-1
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"telos/internal/textjob"
)

// errNoTask means the server had no task for us (HTTP 204).
var errNoTask = errors.New("no task available")

type task struct {
	ID       string          `json:"id"`
	JobID    string          `json:"job_id"`
	Attempts int             `json:"attempts"`
	Input    json.RawMessage `json:"input"`
}

type worker struct {
	server string
	id     string
	client *http.Client
}

func main() {
	hostname, _ := os.Hostname()
	server := flag.String("server", "http://localhost:8080", "address of the Telos server")
	name := flag.String("name", hostname, "this worker's name (must be unique)")
	flag.Parse()

	w := &worker{server: *server, client: &http.Client{Timeout: 10 * time.Second}}

	// 1. Register. Keep trying, so the worker can start before the server.
	for {
		err := w.register(*name)
		if err == nil {
			break
		}
		log.Printf("Could not register (%v); retrying in 3s", err)
		time.Sleep(3 * time.Second)
	}
	log.Printf("Worker %q registered with ID %s", *name, w.id)

	// 2. Work loop.
	for {
		t, err := w.claim()
		if errors.Is(err, errNoTask) {
			time.Sleep(time.Second)
			continue
		}
		if err != nil {
			log.Printf("Could not get a task: %v", err)
			time.Sleep(3 * time.Second)
			continue
		}
		w.process(t)
	}
}

// process runs one task and reports the outcome to the server.
func (w *worker) process(t task) {
	log.Printf("Task %s (job %s, attempt %d): started", t.ID, t.JobID, t.Attempts)
	if err := w.post("/api/worker/tasks/"+t.ID+"/start", nil, nil); err != nil {
		log.Printf("Task %s: could not mark as started: %v", t.ID, err)
		return
	}

	result, err := textjob.Run(t.Input)
	if err != nil {
		log.Printf("Task %s: failed: %v", t.ID, err)
		if err := w.post("/api/worker/tasks/"+t.ID+"/fail", map[string]any{"error": err.Error()}, nil); err != nil {
			log.Printf("Task %s: could not report failure: %v", t.ID, err)
		}
		return
	}

	if err := w.post("/api/worker/tasks/"+t.ID+"/complete", map[string]any{"result": result}, nil); err != nil {
		log.Printf("Task %s: could not send result: %v", t.ID, err)
		return
	}
	log.Printf("Task %s: done, result %s", t.ID, result)
}

func (w *worker) register(name string) error {
	var resp struct {
		ID string `json:"id"`
	}
	if err := w.post("/api/workers/register", map[string]any{"name": name}, &resp); err != nil {
		return err
	}
	w.id = resp.ID
	return nil
}

func (w *worker) claim() (task, error) {
	var t task
	err := w.post("/api/worker/tasks/claim", nil, &t)
	return t, err
}

// post sends body (plus our worker_id) as JSON to the server. If out is not
// nil, the JSON reply is decoded into it. HTTP 204 returns errNoTask.
func (w *worker) post(path string, body map[string]any, out any) error {
	if body == nil {
		body = map[string]any{}
	}
	if w.id != "" {
		body["worker_id"] = w.id
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := w.client.Post(w.server+path, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNoContent:
		if out != nil {
			return errNoTask
		}
		return nil
	case resp.StatusCode >= 300:
		var e struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		return fmt.Errorf("server replied %s: %s", resp.Status, e.Error)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
