package judge

import (
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rokasce/gotyper/engine/lesson"
)

// demoStep is the first step of the repository's lessons: 28 lines of
// handler.go, starting with "package main".
var demoStep = func() lesson.Step {
	lib, err := lesson.Load(os.DirFS("../../lessons"))
	if err != nil {
		panic(err)
	}
	step, ok := lib.Step("json-api/01-greet-handler")
	if !ok {
		panic("json-api/01-greet-handler is missing")
	}
	return step
}()

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

// TestRecallShowsNothing: a recall step hides the target, so a render has no
// ghosts and no red, even for wrong text, and is never done by itself. Only
// the keystrokes and the elapsed time are reported.
func TestRecallShowsNothing(t *testing.T) {
	step := demoStep
	step.Mode = lesson.Recall
	s := NewSession(step)
	now := time.Unix(0, 0)
	s.now = func() time.Time { return now }

	r := s.Update([]string{""}, 0, [2]int{})
	if len(r.Ghosts) != 0 || len(r.GhostLines) != 0 || r.Stats != (Stats{}) {
		t.Fatalf("empty recall buffer: ghosts=%v ghost lines=%d stats=%+v", r.Ghosts, len(r.GhostLines), r.Stats)
	}
	s.Update([]string{"x"}, 1, [2]int{0, 1})
	now = now.Add(3 * time.Second)
	r = s.Update([]string{"xyz", "func"}, 8, [2]int{1, 4})
	if len(r.ErrorSpans) != 0 || len(r.Ghosts) != 0 || len(r.GhostLines) != 0 || r.Done {
		t.Fatalf("wrong recall text: spans=%v ghosts=%v ghost lines=%d done=%v", r.ErrorSpans, r.Ghosts, len(r.GhostLines), r.Done)
	}
	if r.Stats != (Stats{Keys: 8, Seconds: 3}) {
		t.Fatalf("recall stats = %+v, want only keys and seconds", r.Stats)
	}
	if r = s.Update(step.Target, 9, [2]int{}); r.Done {
		t.Fatal("a recall step must not be done by update; a passing check completes it")
	}
}

// drillStep is a small drill: the start file names a variable t, and the
// goal renames it to total.
var drillStep = lesson.Step{
	ID:     "t/01-drill",
	Mode:   lesson.Drill,
	Par:    14,
	Start:  []string{"func sum(nums []int) int {", "\tt := 0", "\tfor _, n := range nums {", "\t\tt += n", "\t}", "\treturn t", "}"},
	Target: []string{"func sum(nums []int) int {", "\ttotal := 0", "\tfor _, n := range nums {", "\t\ttotal += n", "\t}", "\treturn total", "}"},
}

// goalWith returns the drill's goal with row i replaced by the given rows:
// none to delete it, several to insert some.
func goalWith(i int, rows ...string) []string {
	out := append([]string(nil), drillStep.Target[:i]...)
	out = append(out, rows...)
	return append(out, drillStep.Target[i+1:]...)
}

// TestDrillInsertedLine: an extra line marks only that line, not every line
// below it.
func TestDrillInsertedLine(t *testing.T) {
	s := NewSession(drillStep)
	r := s.Update(goalWith(1, "\ttotal := 0", "\tx := 1"), 1, [2]int{})
	if len(r.ErrorSpans) != 1 || r.ErrorSpans[0] != (Span{2, 1, 7}) || r.Done {
		t.Fatalf("inserted line: spans=%+v done=%v", r.ErrorSpans, r.Done)
	}
}

// TestDrillDeletedLine: a missing line leaves the rest unmarked; the buffer
// has nothing to mark for it, and the drill is not done.
func TestDrillDeletedLine(t *testing.T) {
	s := NewSession(drillStep)
	r := s.Update(goalWith(1), 1, [2]int{})
	if len(r.ErrorSpans) != 0 || r.Done {
		t.Fatalf("deleted line: spans=%+v done=%v", r.ErrorSpans, r.Done)
	}
}

// TestDrillEditedLine: on a changed line only the differing run is marked,
// and text that is only missing marks the rune where it is missing.
func TestDrillEditedLine(t *testing.T) {
	s := NewSession(drillStep)
	r := s.Update(goalWith(3, "\t\ttotel += n"), 1, [2]int{})
	// "\t\ttotel += n" against "total += n": only the "e" at byte 5 differs.
	if len(r.ErrorSpans) != 1 || r.ErrorSpans[0] != (Span{3, 5, 6}) {
		t.Fatalf("edited line: spans=%+v", r.ErrorSpans)
	}
	r = s.Update(goalWith(5, "\treturn tot"), 1, [2]int{})
	if len(r.ErrorSpans) != 1 || r.ErrorSpans[0] != (Span{5, 10, 11}) {
		t.Fatalf("shortened line: spans=%+v", r.ErrorSpans)
	}
}

// TestDrillCompletion: the start file shows the renamed rows as differing,
// the timer starts at the first change, and the drill is done, with the
// timer stopped, when the buffer equals the goal (indentation aside).
func TestDrillCompletion(t *testing.T) {
	s := NewSession(drillStep)
	now := time.Unix(0, 0)
	s.now = func() time.Time { return now }

	r := s.Update(drillStep.Start, 0, [2]int{})
	if len(r.ErrorSpans) != 3 || r.ErrorSpans[0] != (Span{1, 2, 2 + len("t")}) || r.Done || r.Stats != (Stats{}) {
		t.Fatalf("start file: spans=%+v done=%v stats=%+v", r.ErrorSpans, r.Done, r.Stats)
	}
	if len(r.Ghosts) != 0 || len(r.GhostLines) != 0 {
		t.Fatalf("a drill shows no ghosts: %+v %v", r.Ghosts, r.GhostLines)
	}
	now = now.Add(time.Second)
	s.Update(goalWith(1, "\ttotal := 0")[:2], 5, [2]int{}) // the first change starts the clock
	now = now.Add(4 * time.Second)
	goal := append([]string(nil), drillStep.Target...)
	goal[3] = "    total += n" // spaces instead of tabs
	if r = s.Update(goal, 14, [2]int{}); !r.Done || len(r.ErrorSpans) != 0 || r.Stats != (Stats{Keys: 14, Seconds: 4}) {
		t.Fatalf("goal: done=%v spans=%+v stats=%+v", r.Done, r.ErrorSpans, r.Stats)
	}
	now = now.Add(time.Minute)
	if r = s.Update(append(goal, "x"), 16, [2]int{}); r.Done || r.Stats.Seconds != 4 {
		t.Fatalf("after done: done=%v stats=%+v", r.Done, r.Stats)
	}
}
