// Package textjob is the text processing job type (TEL-80): it counts the
// words, lines and characters in a piece of text.
//
// It has no database or network code, so the worker can call it directly and
// it is easy to test.
package textjob

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

// Input is what a text task carries: {"content": "..."}.
// It matches what POST /api/jobs stores in jobs.input.
type Input struct {
	Content string `json:"content"`
}

// Result is what a worker sends back, and what GET /api/jobs/{id} shows.
type Result struct {
	Words      int `json:"words"`
	Lines      int `json:"lines"`
	Characters int `json:"characters"`
}

// ErrNoContent means the task input had no text to count.
var ErrNoContent = errors.New("task input has no content")

// Count counts the words, lines and characters in text.
//
//   - words: runs of non-space characters ("hello  world" has 2)
//   - lines: 0 for empty text, otherwise newlines + 1 (a final newline
//     does not start a new line)
//   - characters: letters, digits, spaces and so on, not bytes
//     (so "café" is 4, not 5)
func Count(text string) Result {
	if text == "" {
		return Result{}
	}
	lines := strings.Count(text, "\n") + 1
	if strings.HasSuffix(text, "\n") {
		lines--
	}
	return Result{
		Words:      len(strings.Fields(text)),
		Lines:      lines,
		Characters: utf8.RuneCountInString(text),
	}
}

// Run takes a task's raw JSON input and returns the JSON result, ready to
// send back to the server.
func Run(input json.RawMessage) (json.RawMessage, error) {
	var in Input
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Content) == "" {
		return nil, ErrNoContent
	}
	return json.Marshal(Count(in.Content))
}
