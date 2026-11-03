package model

import (
	"database/sql/driver"
	"fmt"
)

// Priority is the scheduling priority of a job. Tasks inherit the priority
// of their parent job.
type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityNormal Priority = "normal"
	PriorityLow    Priority = "low"
)

// AllPriorities lists priorities from most to least urgent. The scheduler can
// iterate over this slice to visit priority levels in order.
var AllPriorities = []Priority{PriorityHigh, PriorityNormal, PriorityLow}

// Valid reports whether p is a known priority.
func (p Priority) Valid() bool {
	switch p {
	case PriorityHigh, PriorityNormal, PriorityLow:
		return true
	}
	return false
}

// Rank returns a sortable number: lower means more urgent.
// high=0, normal=1, low=2. Unknown priorities rank last.
func (p Priority) Rank() int {
	switch p {
	case PriorityHigh:
		return 0
	case PriorityNormal:
		return 1
	case PriorityLow:
		return 2
	}
	return 99
}

// ParsePriority converts user input into a Priority. An empty string
// defaults to normal so clients may omit the field.
func ParsePriority(s string) (Priority, error) {
	if s == "" {
		return PriorityNormal, nil
	}
	p := Priority(s)
	if !p.Valid() {
		return "", fmt.Errorf("invalid priority %q (want high, normal or low)", s)
	}
	return p, nil
}

func (p Priority) String() string { return string(p) }

// Value implements driver.Valuer so Priority is stored as TEXT.
func (p Priority) Value() (driver.Value, error) {
	if !p.Valid() {
		return nil, fmt.Errorf("invalid priority %q", string(p))
	}
	return string(p), nil
}

// Scan implements sql.Scanner.
func (p *Priority) Scan(src any) error {
	s, err := scanString(src)
	if err != nil {
		return fmt.Errorf("scan priority: %w", err)
	}
	v := Priority(s)
	if !v.Valid() {
		return fmt.Errorf("scan priority: unknown value %q", s)
	}
	*p = v
	return nil
}

// scanString accepts the forms database drivers commonly return for TEXT.
func scanString(src any) (string, error) {
	switch v := src.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	case nil:
		return "", fmt.Errorf("value is NULL")
	}
	return "", fmt.Errorf("unsupported type %T", src)
}
