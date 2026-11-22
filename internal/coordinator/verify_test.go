package coordinator

// TEL-39 end to end: a text job is split, several workers each process
// some of the chunks at the same time, and the combined job result must
// equal counting the whole text on one machine.
//
// Like the other database tests, this needs TELOS_TEST_DATABASE_URL.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"telos/internal/queue"
	"telos/internal/textjob"
)

func TestJobOnSeveralWorkersMatchesSingleMachine(t *testing.T) {
	f := setup(t)
	ctx := context.Background()

	// A text with the awkward bits: line breaks of both kinds, blank lines,
	// tabs, non-English letters and one long line.
	text := strings.Repeat("Héllo, wörld!\tThis is line one.\r\n\nAnd a second line\n", 30) +
		strings.Repeat("one very long line without breaks ", 40)
	id := f.submit(t, text)
	must(t, f.c.Tick(ctx)) // split into tasks

	// Three more workers next to f.worker.
	workers := []string{f.worker}
	for _, name := range []string{"w2", "w3", "w4"} {
		var wid string
		must(t, f.db.QueryRow(
			`INSERT INTO workers (name, token_hash) VALUES ($1, '') RETURNING id`, name).Scan(&wid))
		workers = append(workers, wid)
	}

	// Each worker runs in its own goroutine, claiming tasks until none are left.
	var mu sync.Mutex
	done := map[string]int{} // worker ID -> tasks done
	var wg sync.WaitGroup
	for _, wid := range workers {
		wg.Add(1)
		go func(wid string) {
			defer wg.Done()
			for {
				task, err := f.c.Queue.ClaimTask(ctx, wid)
				if errors.Is(err, queue.ErrEmpty) {
					return
				}
				if err != nil {
					t.Error(err)
					return
				}
				result, err := textjob.Run(task.Input)
				if err != nil {
					t.Error(err)
					return
				}
				if err := f.c.Queue.CompleteTask(ctx, task.ID, wid, result); err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				done[wid]++
				mu.Unlock()
			}
		}(wid)
	}
	wg.Wait()
	must(t, f.c.Tick(ctx)) // combine

	total := 0
	for _, n := range done {
		total += n
	}
	if want := len(textjob.Split(text, textjob.DefaultChunks)); total != want || total < 2 {
		t.Errorf("%d tasks were done, want %d (and more than one)", total, want)
	}
	if len(done) < 2 {
		t.Logf("note: only %d worker(s) got tasks this run", len(done))
	}

	status, result, reason := f.job(t, id)
	if status != "completed" {
		t.Fatalf("job = %s (%s), want completed", status, reason)
	}
	var got textjob.Result
	must(t, json.Unmarshal(result, &got))
	if want := textjob.Count(text); got != want {
		t.Errorf("distributed result %+v differs from single-machine result %+v", got, want)
	}
}
