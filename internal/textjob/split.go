package textjob

// Splitting a text job into chunks, and adding the chunk results back up
// (TEL-37).
//
// Think of slicing a loaf of bread: each worker gets one slice, counts it,
// and the coordinator adds the counts together. For that sum to equal the
// count of the whole loaf (TEL-39), we only ever cut right AFTER a
// whitespace character (space, tab, newline...):
//
//   - a word is never cut in half, so word counts simply add up
//   - a letter like "é" is never cut in half (whitespace is always a single
//     byte), so character counts add up
//   - lines need one small fix, explained in Combine

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// DefaultChunks is how many tasks a text job is split into at most.
	DefaultChunks = 4
	// MinChunkSize (in bytes) stops tiny texts from being cut into
	// pointless crumbs: "hello world" stays one task.
	MinChunkSize = 100
)

// whitespace is where Split may cut. All of these are single bytes.
const whitespace = " \t\n\r\v\f"

// Split cuts text into at most maxChunks pieces of roughly equal size.
// Joining the pieces gives back exactly the original text.
func Split(text string, maxChunks int) []string {
	if maxChunks < 1 {
		maxChunks = 1
	}
	size := (len(text) + maxChunks - 1) / maxChunks // round up
	if size < MinChunkSize {
		size = MinChunkSize
	}

	var chunks []string
	for len(text) > 0 {
		// The last allowed chunk takes whatever is left.
		if len(text) <= size || len(chunks) == maxChunks-1 {
			chunks = append(chunks, text)
			break
		}
		cut := cutPoint(text, size)
		chunks = append(chunks, text[:cut])
		text = text[cut:]
	}
	return mergeBlank(chunks)
}

// cutPoint picks where the first chunk of text should end, aiming for size
// bytes. It never cuts inside a word.
func cutPoint(text string, size int) int {
	// 1. Best: the end of a line, as long as the chunk is at least half size.
	if i := strings.LastIndexByte(text[:size], '\n'); i >= size/2 {
		return i + 1
	}
	// 2. Next best: any whitespace in the second half of the chunk.
	if i := strings.LastIndexAny(text[:size], whitespace); i >= size/2 {
		return i + 1
	}
	// 3. A very long word: carry on to the next whitespace after it.
	if i := strings.IndexAny(text[size:], whitespace); i >= 0 {
		return size + i + 1
	}
	return len(text) // no whitespace left at all: the rest is one chunk
}

// mergeBlank glues chunks that are only whitespace onto a neighbour.
// A worker refuses blank text (ErrNoContent), so such a chunk would make the
// whole job fail even though the text itself is fine.
func mergeBlank(chunks []string) []string {
	var out []string
	carry := "" // blank text at the very start, waiting for the next chunk
	for _, c := range chunks {
		c = carry + c
		carry = ""
		if strings.TrimSpace(c) != "" {
			out = append(out, c)
		} else if len(out) > 0 {
			out[len(out)-1] += c
		} else {
			carry = c
		}
	}
	if carry != "" { // the whole text was blank
		out = append(out, carry)
	}
	return out
}

// Combine adds up the workers' results for the chunks of one text, in
// order, and returns what Count would give for the whole text.
//
// Words and characters just add up. Lines need a fix: when a chunk was cut
// in the middle of a line, both that chunk and the next one count that line,
// so it is counted twice. For every chunk except the last that does not end
// with a newline, we take one line off.
func Combine(chunks []string, results []Result) (Result, error) {
	if len(chunks) != len(results) {
		return Result{}, fmt.Errorf("combine: %d chunks but %d results", len(chunks), len(results))
	}
	var total Result
	for i, r := range results {
		total.Words += r.Words
		total.Lines += r.Lines
		total.Characters += r.Characters
		if i < len(chunks)-1 && !strings.HasSuffix(chunks[i], "\n") {
			total.Lines-- // this line carries on into the next chunk
		}
	}
	return total, nil
}

// SplitInput is Split for a job's raw input ({"content": "..."}): it returns
// one task input per chunk, in the same format.
func SplitInput(input json.RawMessage, maxChunks int) ([]json.RawMessage, error) {
	var in Input
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Content) == "" {
		return nil, ErrNoContent
	}
	chunks := Split(in.Content, maxChunks)
	inputs := make([]json.RawMessage, len(chunks))
	for i, c := range chunks {
		b, err := json.Marshal(Input{Content: c})
		if err != nil {
			return nil, err
		}
		inputs[i] = b
	}
	return inputs, nil
}

// CombineJSON is Combine for raw task inputs and results, as they are
// stored in the tasks table. Both lists must be in task order.
func CombineJSON(inputs, results []json.RawMessage) (json.RawMessage, error) {
	if len(inputs) != len(results) {
		return nil, fmt.Errorf("combine: %d inputs but %d results", len(inputs), len(results))
	}
	chunks := make([]string, len(inputs))
	parts := make([]Result, len(results))
	for i := range inputs {
		var in Input
		if err := json.Unmarshal(inputs[i], &in); err != nil {
			return nil, fmt.Errorf("task %d input: %w", i, err)
		}
		if err := json.Unmarshal(results[i], &parts[i]); err != nil {
			return nil, fmt.Errorf("task %d result: %w", i, err)
		}
		chunks[i] = in.Content
	}
	total, err := Combine(chunks, parts)
	if err != nil {
		return nil, err
	}
	return json.Marshal(total)
}
