package model

import (
	"errors"
	"fmt"
)

// ErrInvalidTransition is returned (wrapped) whenever a state change is not
// allowed. Callers can check for it with errors.Is, e.g. to return HTTP 409.
var ErrInvalidTransition = errors.New("invalid state transition")

// TransitionError describes a rejected state change.
type TransitionError struct {
	Entity string // "job" or "task"
	ID     string
	From   string
	To     string
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("%s %s: cannot move from %q to %q", e.Entity, e.ID, e.From, e.To)
}

func (e *TransitionError) Unwrap() error { return ErrInvalidTransition }
