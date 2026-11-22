package textjob

import (
	"encoding/json"
	"strings"
	"testing"
)

// longText is about 2 KB of short lines, enough to be split.
var longText = strings.Repeat("the quick brown fox jumps over the lazy dog\n", 50)

func TestShortTextIsOneChunk(t *testing.T) {
	chunks := Split("hello world", 4)
	if len(chunks) != 1 || chunks[0] != "hello world" {
		t.Errorf("Split = %q, want one chunk", chunks)
	}
}

func TestLongTextIsSplitIntoSeveralChunks(t *testing.T) {
	chunks := Split(longText, 4)
	if len(chunks) != 4 {
		t.Fatalf("got %d chunks, want 4", len(chunks))
	}
	if strings.Join(chunks, "") != longText {
		t.Error("joining the chunks does not give back the original text")
	}
	for i, c := range chunks[:len(chunks)-1] {
		if !strings.HasSuffix(c, "\n") {
			t.Errorf("chunk %d should end at a line break: %q", i, c[len(c)-10:])
		}
	}
}

func TestNeverMoreChunksThanAsked(t *testing.T) {
	for n := 1; n <= 10; n++ {
		if got := len(Split(longText, n)); got > n {
			t.Errorf("Split(longText, %d) gave %d chunks", n, got)
		}
	}
}

func TestOneLongLineIsCutBetweenWords(t *testing.T) {
	text := strings.Repeat("word ", 200) // 1000 bytes, no newlines
	chunks := Split(text, 4)
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want several", len(chunks))
	}
	for i, c := range chunks[:len(chunks)-1] {
		if !strings.HasSuffix(c, " ") {
			t.Errorf("chunk %d was cut inside a word: ...%q", i, c[len(c)-6:])
		}
	}
}

func TestBlankChunksAreMerged(t *testing.T) {
	// The middle of this text is 300 spaces, which would be a blank chunk.
	text := strings.Repeat("a ", 100) + strings.Repeat(" ", 300) + strings.Repeat("b ", 100)
	chunks := Split(text, 8)
	for i, c := range chunks {
		if strings.TrimSpace(c) == "" {
			t.Errorf("chunk %d is blank", i)
		}
	}
	if strings.Join(chunks, "") != text {
		t.Error("joining the chunks does not give back the original text")
	}
}

func TestCombineFixesLinesCutInTheMiddle(t *testing.T) {
	// "one two" is cut after "one ", so both chunks count that line.
	chunks := []string{"one ", "two\nthree"}
	results := []Result{Count(chunks[0]), Count(chunks[1])}
	got, err := Combine(chunks, results)
	if err != nil {
		t.Fatal(err)
	}
	if want := Count("one two\nthree"); got != want {
		t.Errorf("Combine = %+v, want %+v", got, want)
	}
}

func TestCombineNeedsOneResultPerChunk(t *testing.T) {
	if _, err := Combine([]string{"a", "b"}, []Result{{}}); err == nil {
		t.Error("want an error for a missing result")
	}
}

func TestSplitInputAndCombineJSON(t *testing.T) {
	input, _ := json.Marshal(Input{Content: longText})
	inputs, err := SplitInput(input, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 3 {
		t.Fatalf("got %d task inputs, want 3", len(inputs))
	}
	// Pretend three workers ran the tasks.
	results := make([]json.RawMessage, len(inputs))
	for i, in := range inputs {
		if results[i], err = Run(in); err != nil {
			t.Fatal(err)
		}
	}
	out, err := CombineJSON(inputs, results)
	if err != nil {
		t.Fatal(err)
	}
	var got Result
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if want := Count(longText); got != want {
		t.Errorf("combined result = %+v, want %+v", got, want)
	}
}

func TestSplitInputRejectsBadInput(t *testing.T) {
	if _, err := SplitInput(json.RawMessage(`not json`), 4); err == nil {
		t.Error("invalid JSON: want an error")
	}
	if _, err := SplitInput(json.RawMessage(`{"content":"  "}`), 4); err != ErrNoContent {
		t.Errorf("blank content: want ErrNoContent, got %v", err)
	}
}
