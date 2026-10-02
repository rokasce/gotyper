package stats

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rokasce/gotyper/engine/lesson"
)

func TestAppendThenLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gotyper", FileName) // the directory does not exist yet
	day := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	want := []Record{
		{Step: "t/01-a", Mode: lesson.TypeAlong, WPM: 40, Accuracy: 97.5, Keys: 700, Errors: 3, Seconds: 90, Check: "pass", CheckMS: 900, Time: day},
		{Step: "t/02-b", Mode: lesson.Recall, Keys: 650, Seconds: 120, Check: "pass", CheckMS: 1100, Time: day.Add(time.Hour)},
	}
	for _, r := range want {
		if err := Append(path, r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("loaded %d records, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("record %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestLoadMissingFile(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), FileName))
	if err != nil || len(got) != 0 {
		t.Fatalf("Load of a missing file = %v, %v; want no records and no error", got, err)
	}
}

// TestLoadSkipsBadLines loads a file with a line that is not JSON, one of
// the wrong shape, one without a step, a blank one and a last line cut
// short, as a crash while appending would leave it. Only the good lines
// are kept.
func TestLoadSkipsBadLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	good := `{"step":"t/01-a","mode":"type-along","wpm":40,"keys":700,"time":"2026-10-01T10:00:00Z"}`
	content := good + "\n" +
		"garbage\n" +
		`{"step":42}` + "\n" +
		`{"wpm":50}` + "\n" +
		"\n" +
		good + "\n" +
		good[:20]
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Step != "t/01-a" || got[1].WPM != 40 {
		t.Fatalf("Load = %+v, want the two good lines", got)
	}
}

func TestBests(t *testing.T) {
	day := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	records := []Record{
		{Step: "t/02-b", Mode: lesson.Recall, Keys: 650, Time: day},
		{Step: "t/01-a", Mode: lesson.TypeAlong, WPM: 40, Accuracy: 99, Keys: 720, Time: day},
		{Step: "t/01-a", Mode: lesson.TypeAlong, WPM: 55, Accuracy: 95, Keys: 700, Time: day.Add(2 * time.Hour)},
		// Recorded last, but with an earlier clock: not the latest play.
		{Step: "t/01-a", Mode: lesson.TypeAlong, WPM: 30, Accuracy: 100, Keys: 710, Time: day.Add(time.Hour)},
	}
	got := Bests(records)
	want := []Best{
		{Step: "t/01-a", Mode: lesson.TypeAlong, WPM: 55, Accuracy: 100, Keys: 700, Completions: 3, LastPlayed: day.Add(2 * time.Hour)},
		{Step: "t/02-b", Mode: lesson.Recall, Keys: 650, Completions: 1, LastPlayed: day},
	}
	if len(got) != len(want) {
		t.Fatalf("Bests = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("best %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if got := Bests(nil); got == nil || len(got) != 0 {
		t.Fatalf("Bests(nil) = %#v, want an empty list", got)
	}
}

func TestDefaultPath(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/data")
	if got, _ := DefaultPath(); got != filepath.Join("/data", "gotyper", FileName) {
		t.Errorf("with XDG_DATA_HOME: %s", got)
	}
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "/home/learner")
	if got, _ := DefaultPath(); got != filepath.Join("/home/learner", ".local", "share", "gotyper", FileName) {
		t.Errorf("without XDG_DATA_HOME: %s", got)
	}
}
