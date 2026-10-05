package protocol

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/rokasce/gotyper/engine/lesson"
	"github.com/rokasce/gotyper/engine/stats"
)

// typeAll returns an update request that types the whole target of step in
// one go, as if the learner had just typed its last character.
func typeAll(t *testing.T, id int, step lesson.Step) string {
	t.Helper()
	last := len(step.Target) - 1
	req := Request{ID: new(int64(id)), Op: OpUpdate, Lines: step.Target, Keys: 700, Cursor: [2]int{last, len(step.Target[last])}}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

// TestCompletedStepIsRecorded completes the type-along step, whose check
// then passes, and asks a new engine for the stats: the step is there once.
func TestCompletedStepIsRecorded(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go vet and go test")
	}
	path := filepath.Join(t.TempDir(), "gotyper", "stats.jsonl")
	step, _ := lessons.Step(greetStep)
	resps := serveStats(t, hello+
		`{"id":2,"op":"start","step":"`+greetStep+`"}`+"\n"+
		typeAll(t, 3, step)+
		`{"id":4,"op":"check","lines":`+linesJSON(t, step.Target)+`}`+"\n", path)
	if len(resps) != 4 || !resps[2].Render.Done || resps[3].Check == nil || !resps[3].Check.OK {
		t.Fatalf("completing the step = %+v", resps)
	}
	records, err := stats.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := resps[2].Render.Stats
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1: %+v", len(records), records)
	}
	r := records[0]
	if r.Step != greetStep || r.Mode != lesson.TypeAlong || r.Keys != 700 || r.Errors != 0 ||
		r.WPM != want.WPM || r.Accuracy != 100 || r.Seconds != want.Seconds || r.Check != "pass" || r.Time.IsZero() {
		t.Fatalf("record = %+v, stats were %+v", r, want)
	}

	resps = serveStats(t, hello+`{"id":2,"op":"stats"}`+"\n", path)
	got := resps[1].Stats
	if resps[1].Error != nil || got == nil || len(got.Steps) != 1 {
		t.Fatalf("stats = %+v", resps[1])
	}
	if b := got.Steps[0]; b.Step != greetStep || b.Completions != 1 || b.Keys != 700 || b.Accuracy != 100 || !b.LastPlayed.Equal(r.Time) {
		t.Fatalf("bests = %+v", b)
	}
}

// TestAttemptIsRecordedOnce checks a completed attempt twice, as a second
// F6 or retyping the last character would: both checks pass, but the
// attempt adds one record.
func TestAttemptIsRecordedOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go vet and go test")
	}
	path := filepath.Join(t.TempDir(), "stats.jsonl")
	step, _ := lessons.Step(greetStep)
	check := func(id string) string {
		return `{"id":` + id + `,"op":"check","lines":` + linesJSON(t, step.Target) + `}` + "\n"
	}
	resps := serveStats(t, hello+
		`{"id":2,"op":"start","step":"`+greetStep+`"}`+"\n"+
		typeAll(t, 3, step)+check("4")+check("5"), path)
	if len(resps) != 5 {
		t.Fatalf("got %d responses, want 5", len(resps))
	}
	for _, r := range resps {
		if r.Error != nil || r.Op == OpCheck && !r.Check.OK {
			t.Fatalf("response %d = %+v (check %+v); every check should pass", id(r), r, r.Check)
		}
	}
	records, err := stats.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1: %+v", len(records), records)
	}
}

// TestAbandonedAttemptsAreNotRecorded sends checks that pass but complete
// nothing: one for an attempt restarted while it ran, one for a recall step
// edited while it ran, and one for a type-along step whose text was never
// typed. None of them may add a record.
func TestAbandonedAttemptsAreNotRecorded(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go vet and go test")
	}
	path := filepath.Join(t.TempDir(), "stats.jsonl")
	step, _ := lessons.Step(greetStep)
	recall, _ := lessons.Step(recallStep)
	edited := append(append([]string{}, recall.Target...), "// edited")
	resps := serveStats(t, hello+
		`{"id":2,"op":"start","step":"`+greetStep+`"}`+"\n"+
		typeAll(t, 3, step)+
		`{"id":4,"op":"check","lines":`+linesJSON(t, step.Target)+`}`+"\n"+
		`{"id":5,"op":"restart"}`+"\n"+
		`{"id":6,"op":"check","lines":`+linesJSON(t, step.Target)+`}`+"\n"+
		`{"id":7,"op":"start","step":"`+recallStep+`"}`+"\n"+
		typeAll(t, 8, recall)+
		`{"id":9,"op":"check","lines":`+linesJSON(t, recall.Target)+`}`+"\n"+
		`{"id":10,"op":"update","lines":`+linesJSON(t, edited)+`,"keys":710,"cursor":[0,0]}`+"\n", path)
	if len(resps) != 10 {
		t.Fatalf("got %d responses, want 10", len(resps))
	}
	for _, r := range resps {
		if r.Error != nil || r.Op == OpCheck && !r.Check.OK {
			t.Fatalf("response %d = %+v (check %+v); every check should pass", id(r), r, r.Check)
		}
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		records, _ := stats.Load(path)
		t.Fatalf("nothing was completed, but the stats file holds %+v (stat: %v)", records, err)
	}
}

// TestStatsOp asks for the stats with no stats file, with a partly corrupt
// one, and with one that cannot be read.
func TestStatsOp(t *testing.T) {
	dir := t.TempDir()
	ask := func(path string) Response {
		t.Helper()
		resps := serveStats(t, hello+`{"id":2,"op":"stats"}`+"\n", path)
		if len(resps) != 2 {
			t.Fatalf("got %d responses, want 2", len(resps))
		}
		return resps[1]
	}

	if r := ask(filepath.Join(dir, "missing.jsonl")); r.Error != nil || r.Stats == nil || r.Stats.Steps == nil || len(r.Stats.Steps) != 0 {
		t.Fatalf("stats with no file = %+v", r)
	}

	path := filepath.Join(dir, "stats.jsonl")
	good := `{"step":"` + greetStep + `","mode":"type-along","wpm":40,"accuracy":98,"keys":700,"time":"2026-10-01T10:00:00Z"}`
	if err := os.WriteFile(path, []byte(good+"\nnot json\n"+good[:30]), 0o644); err != nil {
		t.Fatal(err)
	}
	r := ask(path)
	if r.Error != nil || r.Stats == nil || len(r.Stats.Steps) != 1 || r.Stats.Steps[0].Completions != 1 || r.Stats.Steps[0].WPM != 40 {
		t.Fatalf("stats with bad lines = %+v", r)
	}

	// A directory where the file should be cannot be read as one.
	if r := ask(dir); r.Error == nil || r.Error.Code != CodeStatsUnreadable {
		t.Fatalf("stats of an unreadable file = %+v", r)
	}
}

// TestDrillStep plays a drill through the protocol: start opens with the
// start file, the goal and the par; reaching the goal is done and records
// the attempt once, without a check; restart begins at the start file again.
func TestDrillStep(t *testing.T) {
	const drill = "vim-drills/03-move-block"
	step, ok := lessons.Step(drill)
	if !ok {
		t.Fatalf("%s is missing", drill)
	}
	path := filepath.Join(t.TempDir(), "stats.jsonl")
	goal := Request{ID: new(int64(3)), Op: OpUpdate, Lines: step.Target, Keys: 9}
	b, err := json.Marshal(goal)
	if err != nil {
		t.Fatal(err)
	}
	resps := serveStats(t, hello+
		`{"id":2,"op":"start","step":"`+drill+`"}`+"\n"+
		string(b)+"\n"+
		string(b)+"\n"+ // done again: still one record
		`{"id":4,"op":"restart"}`+"\n", path)
	if len(resps) != 5 {
		t.Fatalf("got %d responses, want 5", len(resps))
	}
	st, r := resps[1].Start, resps[1].Render
	if st == nil || st.Mode != lesson.Drill || st.Par != step.Par || len(st.Buffer) != len(step.Start) || len(st.Goal) != len(step.Target) {
		t.Fatalf("drill start = %+v", st)
	}
	if r == nil || r.Done || len(r.ErrorSpans) == 0 {
		t.Fatalf("the start file should differ from the goal: %+v", r)
	}
	if r := resps[2].Render; r == nil || !r.Done || r.Stats.Keys != 9 {
		t.Fatalf("goal update = %+v", r)
	}
	if st, r := resps[4].Start, resps[4].Render; st == nil || len(st.Buffer) != len(step.Start) || r == nil || r.Done || r.Stats.Keys != 0 {
		t.Fatalf("restart = %+v, render %+v", st, r)
	}
	records, err := stats.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Step != drill || records[0].Mode != lesson.Drill || records[0].Keys != 9 || records[0].Check != "" {
		t.Fatalf("records = %+v", records)
	}
}
