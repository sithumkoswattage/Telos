package textjob

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestCount(t *testing.T) {
	cases := []struct {
		name string
		text string
		want Result
	}{
		{"empty", "", Result{0, 0, 0}},
		{"one word", "hello", Result{1, 1, 5}},
		{"extra spaces", "  hello   world  ", Result{2, 1, 17}},
		{"two lines", "one two\nthree", Result{3, 2, 13}},
		{"final newline", "one\ntwo\n", Result{2, 2, 8}},
		{"tabs count as spaces", "a\tb", Result{2, 1, 3}},
		{"non-English letters", "café ok", Result{2, 1, 7}},
	}
	for _, c := range cases {
		if got := Count(c.text); got != c.want {
			t.Errorf("%s: Count(%q) = %+v, want %+v", c.name, c.text, got, c.want)
		}
	}
}

func TestRun(t *testing.T) {
	out, err := Run(json.RawMessage(`{"content":"the quick brown fox"}`))
	if err != nil {
		t.Fatal(err)
	}
	var r Result
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatal(err)
	}
	if r.Words != 4 || r.Lines != 1 {
		t.Errorf("got %+v", r)
	}
}

func TestRunRejectsBadInput(t *testing.T) {
	if _, err := Run(json.RawMessage(`{"content":"   "}`)); !errors.Is(err, ErrNoContent) {
		t.Errorf("blank content: want ErrNoContent, got %v", err)
	}
	if _, err := Run(json.RawMessage(`not json`)); err == nil {
		t.Error("invalid JSON: want an error")
	}
}
