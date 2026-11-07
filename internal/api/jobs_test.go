package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testUserID = "96f2fd54-1afb-4cfe-9116-35c740888656"

// These tests need no database: every case is rejected before a query runs.

func TestListRequiresServerIdentity(t *testing.T) {
	h := &JobsHandler{}
	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/api/jobs", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestListIgnoresClientUserIDWithoutServerIdentity(t *testing.T) {
	h := &JobsHandler{}
	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/api/jobs?user_id=abc", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestGetRequiresServerIdentity(t *testing.T) {
	h := &JobsHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+testUserID, nil)
	req.SetPathValue("id", testUserID)
	h.Get(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestGetRejectsBadJobID(t *testing.T) {
	h := &JobsHandler{DevelopmentUserID: testUserID}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/abc?user_id="+testUserID, nil)
	req.SetPathValue("id", "abc")
	h.Get(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSubmitRejectsInvalidRequestsBeforeDatabase(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "invalid job type",
			body: `{"user_id":"` + testUserID + `","type":"matrix_multiply","priority":"normal","input_method":"text","content":"hello"}`,
			want: "type must be text_processing",
		},
		{
			name: "invalid input method",
			body: `{"user_id":"` + testUserID + `","type":"text_processing","priority":"normal","input_method":"binary","content":"hello"}`,
			want: "input_method must be text or file",
		},
		{
			name: "empty content",
			body: `{"user_id":"` + testUserID + `","type":"text_processing","priority":"normal","input_method":"text","content":"  "}`,
			want: "content is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &JobsHandler{DevelopmentUserID: testUserID}
			rec := httptest.NewRecorder()
			h.Submit(rec, httptest.NewRequest(http.MethodPost, "/api/jobs", strings.NewReader(tt.body)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tt.want) {
				t.Fatalf("body = %q, want %q", rec.Body.String(), tt.want)
			}
		})
	}
}

func TestSubmitRequiresServerIdentity(t *testing.T) {
	h := &JobsHandler{}
	rec := httptest.NewRecorder()
	body := `{"user_id":"` + testUserID + `","type":"text_processing","priority":"normal","input_method":"text","content":"hello"}`
	h.Submit(rec, httptest.NewRequest(http.MethodPost, "/api/jobs", strings.NewReader(body)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestSubmitRejectsOversizedRequest(t *testing.T) {
	body, err := json.Marshal(map[string]string{
		"user_id":      testUserID,
		"type":         "text_processing",
		"priority":     "normal",
		"input_method": "text",
		"content":      strings.Repeat("x", maxSubmitBytes),
	})
	if err != nil {
		t.Fatal(err)
	}

	h := &JobsHandler{DevelopmentUserID: testUserID}
	rec := httptest.NewRecorder()
	h.Submit(rec, httptest.NewRequest(http.MethodPost, "/api/jobs", bytes.NewReader(body)))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}
