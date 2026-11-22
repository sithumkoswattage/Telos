package textjob

// TEL-39: the distributed answer must equal the single-machine answer.
//
// "Single machine" is Count(whole text). "Distributed" is: Split the text,
// Count every chunk separately (as each worker would), then Combine.
// These tests check that the two always agree, for tricky texts and for
// thousands of random ones.

import (
	"math/rand"
	"strings"
	"testing"
)

// distributed does what the coordinator and the workers do together.
func distributed(t *testing.T, text string, chunks int) Result {
	t.Helper()
	parts := Split(text, chunks)
	if strings.Join(parts, "") != text {
		t.Fatalf("Split(%q, %d) lost or changed text", text, chunks)
	}
	results := make([]Result, len(parts))
	for i, p := range parts {
		results[i] = Count(p) // one worker per chunk
	}
	got, err := Combine(parts, results)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestDistributedMatchesSingleMachine(t *testing.T) {
	long := func(s string, n int) string { return strings.Repeat(s, n) }
	texts := map[string]string{
		"short":                  "hello world",
		"many lines":             long("the quick brown fox\n", 80),
		"no final newline":       long("line\n", 60) + "last line",
		"one long line":          long("word ", 400),
		"windows line endings":   long("one two\r\n", 70),
		"blank lines":            long("a\n\n\n", 60),
		"tabs and spaces":        long("a\t b  \t c ", 120),
		"non-English letters":    long("café naïve 日本語 ", 60),
		"lots of spaces":         long("x", 50) + long(" ", 500) + long("y ", 50),
		"one huge word":          long("z", 1000) + " end",
		"leading blank":          long(" ", 300) + long("word ", 60),
		"newline only at ends":   "\n" + long("w ", 200) + "\n",
		"mix of everything here": long("Héllo,\tworld!\r\n\n  ok ", 50),
	}
	for name, text := range texts {
		want := Count(text)
		for n := 1; n <= 10; n++ {
			if got := distributed(t, text, n); got != want {
				t.Errorf("%s, %d chunks: got %+v, want %+v", name, n, got, want)
			}
		}
	}
}

// TestDistributedMatchesSingleMachineRandom builds random texts out of
// awkward pieces and checks every one.
func TestDistributedMatchesSingleMachineRandom(t *testing.T) {
	pieces := []string{"a", "word", "é", "日本", " ", "  ", "\t", "\n", "\r\n", "\n\n", "x y"}
	rng := rand.New(rand.NewSource(39)) // fixed seed: the same texts every run
	for i := 0; i < 2000; i++ {
		var b strings.Builder
		size := rng.Intn(3000)
		for b.Len() < size {
			b.WriteString(pieces[rng.Intn(len(pieces))])
		}
		text := b.String()
		if strings.TrimSpace(text) == "" {
			continue // the API refuses blank jobs
		}
		n := 1 + rng.Intn(10)
		if got, want := distributed(t, text, n), Count(text); got != want {
			t.Fatalf("text #%d (%d bytes, %d chunks): got %+v, want %+v", i, len(text), n, got, want)
		}
	}
}
