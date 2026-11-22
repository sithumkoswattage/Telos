package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// resultsFixture makes two users and one job owned by the first.
func resultsFixture(t *testing.T) (h *JobsHandler, owner, stranger, jobID string) {
	t.Helper()
	database := openAPIJobTestDB(t)
	if err := database.QueryRow(`INSERT INTO users (email, password_hash) VALUES ('owner@test', 'x') RETURNING id::text`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`INSERT INTO users (email, password_hash) VALUES ('other@test', 'x') RETURNING id::text`).Scan(&stranger); err != nil {
		t.Fatal(err)
	}
	h = &JobsHandler{DB: database, DevelopmentUserID: owner}

	// Submit through the real handler, so the job.submitted event is written too.
	rec := httptest.NewRecorder()
	h.Submit(rec, httptest.NewRequest(http.MethodPost, "/api/jobs", strings.NewReader(
		`{"type":"text_processing","priority":"high","input_method":"text","content":"one two three"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct{ ID string }
	json.Unmarshal(rec.Body.Bytes(), &resp)
	return h, owner, stranger, resp.ID
}

func get(h http.HandlerFunc, path, jobID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.SetPathValue("id", jobID)
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestResultEndpoint(t *testing.T) {
	h, _, stranger, jobID := resultsFixture(t)
	url := "/api/jobs/" + jobID + "/result"

	// Still queued -> 409.
	if rec := get(h.Result, url, jobID); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), "not finished") {
		t.Errorf("queued: %d %s", rec.Code, rec.Body.String())
	}

	// Failed -> 409 with the reason.
	h.DB.Exec(`UPDATE jobs SET status = 'failed', error = 'boom' WHERE id = $1`, jobID)
	if rec := get(h.Result, url, jobID); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("failed: %d %s", rec.Code, rec.Body.String())
	}

	// Completed -> 200 with the result.
	h.DB.Exec(`UPDATE jobs SET status = 'completed', error = '', completed_at = now(),
	           result = '{"words":3,"lines":1,"characters":13}' WHERE id = $1`, jobID)
	rec := get(h.Result, url, jobID)
	if rec.Code != http.StatusOK {
		t.Fatalf("completed: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		JobID  string `json:"job_id"`
		Result struct{ Words int }
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body.JobID != jobID || body.Result.Words != 3 {
		t.Errorf("completed body = %s", rec.Body.String())
	}
	if rec.Header().Get("Content-Disposition") != "" {
		t.Error("plain request should not be a download")
	}

	// ?download=1 -> same reply, saved as a file.
	rec = get(h.Result, url+"?download=1", jobID)
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") ||
		!strings.Contains(cd, "telos-result-"+jobID[:8]+".json") {
		t.Errorf("download header = %q", cd)
	}

	// Someone else's job -> 404, as if it did not exist.
	other := &JobsHandler{DB: h.DB, DevelopmentUserID: stranger}
	if rec := get(other.Result, url, jobID); rec.Code != http.StatusNotFound {
		t.Errorf("stranger: %d, want 404", rec.Code)
	}
}

func TestEventsEndpoint(t *testing.T) {
	h, _, stranger, jobID := resultsFixture(t)
	url := "/api/jobs/" + jobID + "/events"

	rec := get(h.Events, url, jobID)
	if rec.Code != http.StatusOK {
		t.Fatalf("events: %d %s", rec.Code, rec.Body.String())
	}
	var list []struct{ Type, Message string }
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].Type != "job.submitted" || !strings.Contains(list[0].Message, "high") {
		t.Errorf("events = %s, want one job.submitted with high priority", rec.Body.String())
	}

	other := &JobsHandler{DB: h.DB, DevelopmentUserID: stranger}
	if rec := get(other.Events, url, jobID); rec.Code != http.StatusNotFound {
		t.Errorf("stranger: %d, want 404", rec.Code)
	}
}
