package judge

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/rokasce/gotyper/engine/lesson"
)

var demoStep = lesson.Fixture()

func TestPerfectStepIsDone(t *testing.T) {
	s := NewSession(demoStep)
	r := s.Update(demoStep.Target, 0, [2]int{})
	if !r.Done || len(r.ErrorSpans) != 0 || len(r.Ghosts) != 0 || len(r.GhostLines) != 0 {
		t.Fatalf("perfect input: done=%v errors=%v ghosts=%v ghost lines=%d", r.Done, r.ErrorSpans, r.Ghosts, len(r.GhostLines))
	}
}

func TestIndentationIsLenient(t *testing.T) {
	s := NewSession(demoStep)
	lines := append([]string(nil), demoStep.Target...)
	for i, l := range lines {
		lines[i] = strings.ReplaceAll(l, "\t", "  ") // spaces instead of tabs
	}
	if r := s.Update(lines, 0, [2]int{}); !r.Done || len(r.ErrorSpans) != 0 {
		t.Fatalf("space-indented input should pass: %+v", r.ErrorSpans)
	}
}

func TestMistakesAndGhosts(t *testing.T) {
	s := NewSession(demoStep)
	s.Update([]string{"packx"}, 0, [2]int{0, 5})
	r := s.Update([]string{"packxge"}, 0, [2]int{0, 7})
	if len(r.ErrorSpans) != 1 || r.ErrorSpans[0] != (Span{0, 4, 5}) {
		t.Fatalf("errors = %+v", r.ErrorSpans)
	}
	if len(r.Ghosts) != 1 || r.Ghosts[0] != (Ghost{0, 7, " main"}) {
		t.Fatalf("ghosts = %+v", r.Ghosts)
	}
	if len(r.GhostLines) != len(demoStep.Target)-1 {
		t.Fatalf("ghost lines = %d", len(r.GhostLines))
	}
	if r.GhostLines[2] != `    "encoding/json"` {
		t.Fatalf("ghost line tabs not expanded: %q", r.GhostLines[2])
	}
	if r.Stats.Errors != 1 {
		t.Fatalf("errors stat = %d", r.Stats.Errors)
	}
	// Same wrong char still there: not a new error. Fixing it: still 1 total.
	s.Update([]string{"packxge "}, 0, [2]int{0, 8})
	r = s.Update([]string{"package "}, 0, [2]int{0, 8})
	if r.Stats.Errors != 1 || len(r.ErrorSpans) != 0 {
		t.Fatalf("after fix: errors stat=%d spans=%v", r.Stats.Errors, r.ErrorSpans)
	}
}

func TestAdjacentWrongBytesMergeIntoOneSpan(t *testing.T) {
	s := NewSession(demoStep)
	r := s.Update([]string{"pacXYZ"}, 0, [2]int{0, 6})
	if len(r.ErrorSpans) != 1 || r.ErrorSpans[0] != (Span{0, 3, 6}) {
		t.Fatalf("errors = %+v", r.ErrorSpans)
	}
}

func TestShiftedTextIsNotChargedAgain(t *testing.T) {
	s := NewSession(demoStep)
	lines := []string{"package main", "", "imoprt ("}
	s.Update(lines, 0, [2]int{2, 3}) // typo typed: 'o' at the cursor
	// cw over the word, then retype it one char at a time: the " (" tail
	// shifts through wrong positions but the cursor is never on it.
	for _, l := range []string{" (", "i (", "im (", "imp (", "impo (", "impor (", "import ("} {
		lines[2] = l
		s.Update(lines, 0, [2]int{2, len(l) - 2})
	}
	if r := s.Update(lines, 0, [2]int{2, 8}); r.Stats.Errors != 1 || len(r.ErrorSpans) != 0 {
		t.Fatalf("errors stat = %d (want 1), spans = %v", r.Stats.Errors, r.ErrorSpans)
	}
}

func TestEmptyLineGhostIncludesIndent(t *testing.T) {
	s := NewSession(demoStep)
	lines := append([]string(nil), demoStep.Target[:3]...)
	lines = append(lines, "")
	r := s.Update(lines, 0, [2]int{})
	if len(r.Ghosts) != 1 || r.Ghosts[0].Text != `    "encoding/json"` {
		t.Fatalf("ghosts = %+v", r.Ghosts)
	}
	lines[3] = "\t"
	r = s.Update(lines, 0, [2]int{})
	if r.Ghosts[0] != (Ghost{3, 1, `"encoding/json"`}) {
		t.Fatalf("auto-indented ghost = %+v", r.Ghosts[0])
	}
}

func TestExtraLinesAreErrors(t *testing.T) {
	s := NewSession(demoStep)
	lines := append(append([]string(nil), demoStep.Target...), "", "oops")
	r := s.Update(lines, 0, [2]int{})
	if r.Done || len(r.ErrorSpans) != 1 || r.ErrorSpans[0].Row != len(demoStep.Target)+1 {
		t.Fatalf("done=%v errors=%+v", r.Done, r.ErrorSpans)
	}
}

func TestWPM(t *testing.T) {
	s := NewSession(demoStep)
	now := time.Unix(0, 0)
	s.now = func() time.Time { return now }
	s.Update([]string{"p"}, 1, [2]int{0, 1})
	now = now.Add(6 * time.Second)
	r := s.Update([]string{"package main", "", "impor"}, 20, [2]int{2, 5})
	// 12 + 0 + 5 runes + 2 newlines = 19 correct chars in 0.1 min.
	if r.Stats.Correct != 19 || math.Round(r.Stats.WPM) != 38 {
		t.Fatalf("stats = %+v", r.Stats)
	}
}

func TestIndentsAndWidth(t *testing.T) {
	s := NewSession(demoStep)
	in := s.Indents()
	if len(in) != len(demoStep.Target) || in[0] != 0 || in[3] != 4 || in[14] != 8 {
		t.Fatalf("indents = %v", in)
	}
	// "\t\treturn greeting{}, errors.New(\"name is required\")" with two
	// tabs expanded to 8 columns.
	if w := s.Width(); w != 59 {
		t.Fatalf("width = %d", w)
	}
}

func TestRestartClearsAttempt(t *testing.T) {
	s := NewSession(demoStep)
	now := time.Unix(0, 0)
	s.now = func() time.Time { return now }
	s.Update([]string{"x"}, 1, [2]int{0, 1}) // one charged mistake
	now = now.Add(10 * time.Second)
	if r := s.Update([]string{"x"}, 2, [2]int{0, 1}); r.Stats.Errors != 1 || r.Stats.Seconds != 10 {
		t.Fatalf("before restart: %+v", r.Stats)
	}

	s.Restart()
	r := s.Update([]string{""}, 0, [2]int{})
	if r.Stats != (Stats{Accuracy: 100, Lines: len(demoStep.Target)}) {
		t.Fatalf("fresh attempt stats = %+v", r.Stats)
	}
	// The timer restarts at the first character typed after the restart.
	now = now.Add(5 * time.Second)
	s.Update([]string{"p"}, 1, [2]int{0, 1})
	now = now.Add(2 * time.Second)
	if r = s.Update([]string{"pa"}, 2, [2]int{0, 2}); r.Stats.Seconds != 2 {
		t.Fatalf("timer not reset: %+v", r.Stats)
	}
	// The old mistake history is gone: the same typo is charged again.
	if r = s.Update([]string{"x"}, 3, [2]int{0, 1}); r.Stats.Errors != 1 {
		t.Fatalf("errors after retyping the typo = %d", r.Stats.Errors)
	}
}
