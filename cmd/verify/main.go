// Command verify checks that Telos gives the same answer as one machine
// (TEL-39).
//
// It counts a text file locally, submits the same text as a job, waits for
// the workers to finish, and compares the two results:
//
//	go run ./cmd/verify -file cmd/verify/sample.txt
//
// Start the server and a few workers first (see the README). It exits with
// status 1 if the results differ, so it can also be used in scripts.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"telos/internal/textjob"
)

func main() {
	server := flag.String("server", "http://localhost:8080", "address of the Telos server")
	file := flag.String("file", "cmd/verify/sample.txt", "text file to process")
	timeout := flag.Duration("timeout", time.Minute, "how long to wait for the job")
	flag.Parse()

	data, err := os.ReadFile(*file)
	if err != nil {
		fail("read file: %v", err)
	}
	text := string(data)

	// 1. Single machine: count it right here.
	want := textjob.Count(text)

	// 2. Distributed: submit it and let the workers do it.
	id, err := submit(*server, filepath.Base(*file), text)
	if err != nil {
		fail("submit job: %v", err)
	}
	fmt.Printf("Submitted job %s, waiting for the workers...\n", id)
	got, err := wait(*server, id, *timeout)
	if err != nil {
		fail("job %s: %v", id, err)
	}

	// 3. Compare.
	fmt.Println()
	fmt.Printf("%-12s %14s %14s\n", "", "single machine", "Telos")
	row := func(label string, a, b int) {
		mark := "ok"
		if a != b {
			mark = "DIFFERENT"
		}
		fmt.Printf("%-12s %14d %14d   %s\n", label, a, b, mark)
	}
	row("words", want.Words, got.Words)
	row("lines", want.Lines, got.Lines)
	row("characters", want.Characters, got.Characters)
	fmt.Println()

	if got != want {
		fail("the results differ")
	}
	fmt.Println("PASS: the distributed result matches the single-machine result")
}

// submit sends the text as a new job and returns its ID.
func submit(server, fileName, text string) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"type":         "text_processing",
		"priority":     "normal",
		"input_method": "file",
		"content":      text,
		"file_name":    fileName,
	})
	resp, err := http.Post(server+"/api/jobs", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("server replied %s: %s", resp.Status, out.Error)
	}
	return out.ID, nil
}

// wait asks about the job every half second until it is finished.
func wait(server, id string, timeout time.Duration) (textjob.Result, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(server + "/api/jobs/" + id)
		if err != nil {
			return textjob.Result{}, err
		}
		var job struct {
			Status string          `json:"status"`
			Error  string          `json:"error"`
			Result json.RawMessage `json:"result"`
		}
		err = json.NewDecoder(resp.Body).Decode(&job)
		resp.Body.Close()
		if err != nil {
			return textjob.Result{}, err
		}

		switch job.Status {
		case "completed":
			var r textjob.Result
			err := json.Unmarshal(job.Result, &r)
			return r, err
		case "failed", "cancelled":
			return textjob.Result{}, fmt.Errorf("job %s: %s", job.Status, job.Error)
		}
		time.Sleep(500 * time.Millisecond)
	}
	return textjob.Result{}, fmt.Errorf("not finished after %s (are workers running?)", timeout)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAIL: "+format+"\n", args...)
	os.Exit(1)
}
