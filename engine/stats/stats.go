// Package stats keeps the learner's results between games. Every completed
// step adds one line to a file, stats.jsonl, and the engine reads that file
// back to report each step's bests.
//
// The file is JSON Lines: one JSON object per line, the same framing as the
// protocol. Appending a line never rewrites what is already there, so a
// crash while writing can damage at most the last line, and Load skips a
// damaged line instead of giving up on the whole file.
package stats

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/rokasce/gotyper/engine/lesson"
)

// FileName is the name of the stats file inside its directory.
const FileName = "stats.jsonl"

// Record is one completed step: one line of the stats file. Only completed
// attempts are recorded; a restarted or abandoned attempt leaves no line.
type Record struct {
	// Step is the step's id, such as "json-api/01-greet-handler".
	Step string      `json:"step"`
	Mode lesson.Mode `json:"mode"`
	// WPM, Accuracy, Keys, Errors and Seconds are the attempt's final
	// stats, as the update op reported them (judge.Stats). In a recall
	// step WPM and Accuracy are 0, because there is no target to compare
	// against.
	WPM      float64 `json:"wpm"`
	Accuracy float64 `json:"accuracy"`
	Keys     int     `json:"keys"`
	Errors   int     `json:"errors"`
	Seconds  float64 `json:"seconds"`
	// Check is the result of the check that completed the step. Only a
	// passing check completes a step, so it is always "pass"; CheckMS is
	// how long that check took.
	Check   string `json:"check"`
	CheckMS int64  `json:"check_ms"`
	// Time is when the step was completed.
	Time time.Time `json:"time"`
}

// Best sums up every record of one step: the best result in each stat and
// how often the step was completed.
type Best struct {
	Step string `json:"step"`
	// Mode is the mode of the step's latest record.
	Mode lesson.Mode `json:"mode"`
	// WPM and Accuracy are the highest seen, and Keys the fewest
	// keystrokes. Each comes from whichever attempt was best at it, so
	// they need not come from the same attempt.
	WPM      float64 `json:"best_wpm"`
	Accuracy float64 `json:"best_accuracy"`
	Keys     int     `json:"fewest_keys"`
	// Completions counts the step's records.
	Completions int `json:"completions"`
	// LastPlayed is the time of the latest record.
	LastPlayed time.Time `json:"last_played"`
}

// DefaultPath returns where the stats file lives: gotyper/stats.jsonl under
// $XDG_DATA_HOME, or under ~/.local/share when that is not set. That is the
// directory Neovim keeps its own data in (stdpath("data") is its nvim/
// subdirectory), so the learner's data sits in one familiar place.
func DefaultPath() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("finding the stats directory: %w", err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "gotyper", FileName), nil
}

// Append adds r to the stats file at path as one line, creating the file and
// its directory if needed.
func Append(path string, r Record) error {
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// O_APPEND makes every write go to the end of the file, even if another
	// engine appended a line since this one opened it.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	// One Write call for the whole line, so it is not interleaved with
	// another writer's line.
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Load reads every record in the stats file at path, oldest first. A missing
// file means nothing was completed yet, so it returns no records and no
// error. A line that is not a valid record, such as one cut short by a
// crash or edited by hand, is skipped. Only failing to read the file at all
// is an error.
func Load(path string) ([]Record, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var records []Record
	in := bufio.NewReader(f)
	for {
		// ReadBytes, like the protocol's reader, has no line-length limit.
		line, err := in.ReadBytes('\n')
		var r Record
		if json.Unmarshal(line, &r) == nil && r.Step != "" {
			records = append(records, r)
		}
		if errors.Is(err, io.EOF) {
			return records, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// Bests sums up records per step, sorted by step id. Ids start with the
// track and the step's NN- number, so that is play order within a track.
func Bests(records []Record) []Best {
	byStep := map[string]*Best{}
	for _, r := range records {
		b := byStep[r.Step]
		if b == nil {
			b = &Best{Step: r.Step, WPM: r.WPM, Accuracy: r.Accuracy, Keys: r.Keys}
			byStep[r.Step] = b
		}
		b.WPM = max(b.WPM, r.WPM)
		b.Accuracy = max(b.Accuracy, r.Accuracy)
		b.Keys = min(b.Keys, r.Keys)
		b.Completions++
		// The file is in completion order, but a clock can be changed, so
		// the latest is decided by the time itself.
		if !r.Time.Before(b.LastPlayed) {
			b.LastPlayed, b.Mode = r.Time, r.Mode
		}
	}
	bests := make([]Best, 0, len(byStep)) // [] rather than null when empty
	for _, b := range byStep {
		bests = append(bests, *b)
	}
	sort.Slice(bests, func(i, j int) bool { return bests[i].Step < bests[j].Step })
	return bests
}
