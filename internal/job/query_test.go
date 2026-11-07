package job

import (
	"context"
	"errors"
	"testing"
)

const testUUID = "96f2fd54-1afb-4cfe-9116-35c740888656"

// These tests need no database: bad ids are rejected before any query runs.

func TestListRejectsBadUserID(t *testing.T) {
	_, err := List(context.Background(), nil, "not-a-uuid")
	if !errors.Is(err, ErrInvalidUserID) {
		t.Fatalf("err = %v, want ErrInvalidUserID", err)
	}
}

func TestGetRejectsBadUserID(t *testing.T) {
	_, err := Get(context.Background(), nil, testUUID, "nope")
	if !errors.Is(err, ErrInvalidUserID) {
		t.Fatalf("err = %v, want ErrInvalidUserID", err)
	}
}

func TestGetRejectsBadJobID(t *testing.T) {
	_, err := Get(context.Background(), nil, "nope", testUUID)
	if !errors.Is(err, ErrInvalidJobID) {
		t.Fatalf("err = %v, want ErrInvalidJobID", err)
	}
}
