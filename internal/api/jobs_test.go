package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const testUserID = "96f2fd54-1afb-4cfe-9116-35c740888656"

// These tests need no database: every case is rejected before a query runs.

func TestListRequiresUserID(t *testing.T) {
	h := &JobsHandler{}
	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/api/jobs", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestListRejectsBadUserID(t *testing.T) {
	h := &JobsHandler{}
	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/api/jobs?user_id=abc", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestGetRequiresUserID(t *testing.T) {
	h := &JobsHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+testUserID, nil)
	req.SetPathValue("id", testUserID)
	h.Get(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestGetRejectsBadJobID(t *testing.T) {
	h := &JobsHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/abc?user_id="+testUserID, nil)
	req.SetPathValue("id", "abc")
	h.Get(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
