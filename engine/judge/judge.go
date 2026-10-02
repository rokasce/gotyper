// Package judge compares what the learner has typed with the step's target
// code and turns the result into something the front end can paint: which
// bytes are wrong, which target text is still untyped, and the live stats.
//
// The judge never looks at Neovim. It receives the whole buffer as a slice of
// lines on every change and recomputes everything from scratch; a 28-line
// step takes around ten microseconds, so there is no need to be incremental.
package judge

// Span is a range of wrong bytes on one buffer row. Row is 0-based; Col and
// EndCol are 0-based byte offsets into that row, with EndCol exclusive, which
// is exactly how Neovim's extmark API addresses text.
type Span struct {
	Row    int `json:"row"`
	Col    int `json:"col"`
	EndCol int `json:"end_col"`
}

// Ghost is untyped target text that the front end draws inline, right after
// what the learner has typed on that row. Col is the byte offset where the
// ghost starts, which is always the end of the typed line.
type Ghost struct {
	Row  int    `json:"row"`
	Col  int    `json:"col"`
	Text string `json:"text"`
}

// Stats is the scoreboard for the current attempt. In a recall step only Keys
// and Seconds are filled in; with no target to compare against, the rest has
// no meaning and stays 0.
type Stats struct {
	// WPM is words per minute: correct characters / 5 / minutes elapsed.
	// It stays 0 for the first second so a single fast key doesn't show
	// an absurd number.
	WPM float64 `json:"wpm"`
	// Accuracy is 100 * correct / (correct + errors), in percent.
	Accuracy float64 `json:"accuracy"`
	// Keys is the keystroke count the front end reported for this attempt.
	Keys int `json:"keys"`
	// Correct counts characters that currently match the target, plus one
	// for each newline between typed rows.
	Correct int `json:"correct"`
	// Errors counts mistakes charged so far in this attempt. It never goes
	// down when a mistake is fixed: fixing a typo doesn't un-make it.
	Errors int `json:"errors"`
	// Line is how many leading lines fully match; Lines is the target's
	// line count. Together they read as "line 12/28".
	Line  int `json:"line"`
	Lines int `json:"lines"`
	// Seconds is the time since the first typed character, frozen once the
	// attempt is done.
	Seconds float64 `json:"seconds"`
}

// Render is everything the front end needs to paint one buffer state.
type Render struct {
	// ErrorSpans are the wrong bytes to highlight, merged so that a run
	// of adjacent wrong bytes on a row is a single span.
	ErrorSpans []Span `json:"error_spans"`
	// Ghosts are the untyped remainders of rows the learner has started
	// (at most one per row).
	Ghosts []Ghost `json:"ghosts"`
	// GhostLines are whole target rows below the end of the buffer that the
	// learner hasn't reached yet, with tabs expanded to spaces so they can
	// be drawn as plain virtual text.
	GhostLines []string `json:"ghost_lines"`
	Stats      Stats    `json:"stats"`
	// Done reports whether the attempt has met the step's completion
	// condition. For a type-along step, that means every line matches the
	// target exactly (indentation aside). For a recall step it is always
	// false: a passing check completes it instead.
	Done bool `json:"done"`
	// ComputeUS is how long the judge took, in microseconds, so the front
	// end can tell judging time apart from bridge and paint time.
	ComputeUS int64 `json:"compute_us"`
}
