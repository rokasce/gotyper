package judge

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rokasce/gotyper/engine/lesson"
)

// errKey identifies one mistake: the wrong rune got at target rune index idx
// on a row. Remembering the keys seen on the previous update is how the
// session tells a mistake that is still on screen from a brand-new one.
type errKey struct {
	row, idx int
	r        rune
}

// Session judges one learner's attempt at one step. It is not safe for
// concurrent use; the protocol server calls it from a single goroutine.
//
// Indentation is lenient: leading spaces and tabs are stripped from both the
// typed line and the target line before comparing, so tabs-vs-spaces or the
// front end's auto-indent can never count as a mistake.
type Session struct {
	step    lesson.Step
	target  []string // step.Target with leading whitespace removed
	started time.Time
	ended   time.Time
	prevErr map[errKey]bool // mistakes visible on the previous update
	errors  int             // mistakes charged so far
	now     func() time.Time
}

// NewSession starts a fresh attempt at step.
func NewSession(step lesson.Step) *Session {
	s := &Session{step: step, now: time.Now}
	for _, l := range step.Target {
		s.target = append(s.target, strings.TrimLeft(l, " \t"))
	}
	s.Restart()
	return s
}

// Restart throws away the current attempt and begins a new one at the same
// step: the error count and mistake history are cleared and the timer is
// reset, so the next Update scores as if the learner had just arrived.
func (s *Session) Restart() {
	s.started = time.Time{}
	s.ended = time.Time{}
	s.prevErr = map[errKey]bool{}
	s.errors = 0
}

// Step returns the step this session is judging.
func (s *Session) Step() lesson.Step {
	return s.step
}

// Indents returns, for every target line, its indentation in display columns
// (a tab counts up to the next multiple of 4). The front end uses this table
// to auto-indent when the learner presses Enter, so the learner never types
// indentation themselves.
func (s *Session) Indents() []int {
	out := make([]int, len(s.step.Target))
	for i, l := range s.step.Target {
		out[i] = displayWidth(leadingWhitespace(l, s.target[i]))
	}
	return out
}

// Width returns the display width of the widest target line, tabs expanded,
// so the front end can lay out panels beside the code.
func (s *Session) Width() int {
	w := 0
	for _, l := range s.step.Target {
		w = max(w, utf8.RuneCountInString(expandTabs(l, 0)))
	}
	return w
}

// Update judges one buffer state and returns what to paint.
//
//   - lines is the whole buffer, one string per row.
//   - keys is the front end's keystroke count for this attempt; the judge
//     only reports it back in the stats.
//   - cursor is {row, byteCol}, 0-based, of the insert position. It decides
//     whether a mistake is charged (see the at-cursor rule below).
//
// How the diff works: each typed row is compared with the target row of the
// same number, rune by rune, after both have had their indentation stripped.
// There is no alignment or "best match" search: the n-th typed rune is
// compared with the n-th target rune. Every mismatching rune becomes an error
// span (in byte offsets, because that is what Neovim highlights by). Whatever
// part of the target row lies past the end of the typed row becomes that row's
// ghost, and target rows past the end of the buffer become ghost lines.
func (s *Session) Update(lines []string, keys int, cursor [2]int) Render {
	t0 := time.Now()
	r := Render{ErrorSpans: []Span{}, Ghosts: []Ghost{}, GhostLines: []string{}}
	curErr := map[errKey]bool{}
	correct, typedAny := 0, false
	done := true

	for row, line := range lines {
		content := strings.TrimLeft(line, " \t")
		off := len(line) - len(content) // byte width of the typed indentation
		if content != "" {
			typedAny = true
		}
		if row >= len(s.target) {
			// The buffer is longer than the target. Blank extra rows are
			// harmless (a trailing Enter); anything else is all wrong.
			if strings.TrimSpace(line) != "" {
				r.ErrorSpans = append(r.ErrorSpans, Span{row, 0, len(line)})
				done = false
			}
			continue
		}
		want := s.target[row]
		if content != want {
			done = false
		}
		wantRunes := []rune(want)
		// idx walks the target in runes; col walks the typed line in bytes.
		// They advance together, one rune at a time.
		idx, col := 0, off
		for col < len(line) {
			got, size := utf8.DecodeRuneInString(line[col:])
			if idx < len(wantRunes) && got == wantRunes[idx] {
				correct++
			} else {
				k := errKey{row, idx, got}
				curErr[k] = true
				// The at-cursor charging rule. A mistake costs accuracy
				// only if it is new (not on screen last update) AND it
				// ends exactly at the cursor, meaning the learner just
				// typed it. Without the cursor check, an edit in the
				// middle of a line (say `cw` to retype a word) shifts the
				// rest of the line through wrong positions on every key,
				// and each shifted character would be charged as a fresh
				// mistake: one real typo used to cost 14 errors that way.
				// The shifted text is still painted red; it just isn't
				// charged.
				atCursor := row == cursor[0] && col+size == cursor[1]
				if !s.prevErr[k] && atCursor {
					s.errors++
				}
				// Extend the previous span if this wrong byte touches it,
				// so a wrong word is one highlight, not one per letter.
				if n := len(r.ErrorSpans); n > 0 && r.ErrorSpans[n-1].Row == row && r.ErrorSpans[n-1].EndCol == col {
					r.ErrorSpans[n-1].EndCol = col + size
				} else {
					r.ErrorSpans = append(r.ErrorSpans, Span{row, col, col + size})
				}
			}
			idx++
			col += size
		}
		if idx < len(wantRunes) {
			ghost := string(wantRunes[idx:])
			if line == "" {
				// Nothing typed on this row yet, not even the auto-indent:
				// prepend the target's indentation as spaces so the ghost
				// still shows where the line goes.
				ghost = expandTabs(leadingWhitespace(s.step.Target[row], want), 0) + ghost
			}
			r.Ghosts = append(r.Ghosts, Ghost{row, len(line), ghost})
		}
		if row > 0 {
			correct++ // the newline that got the learner onto this row
		}
	}
	for row := len(lines); row < len(s.step.Target); row++ {
		done = false
		r.GhostLines = append(r.GhostLines, expandTabs(s.step.Target[row], 0))
	}
	s.prevErr = curErr

	// The clock starts at the first typed character and stops the first
	// time the attempt is done.
	if typedAny && s.started.IsZero() {
		s.started = s.now()
	}
	if done && s.ended.IsZero() {
		s.ended = s.now()
	}
	end := s.ended
	if end.IsZero() {
		end = s.now()
	}

	st := Stats{Keys: keys, Correct: correct, Errors: s.errors, Lines: len(s.target)}
	for i, l := range lines {
		if i >= len(s.target) || strings.TrimLeft(l, " \t") != s.target[i] {
			break
		}
		st.Line = i + 1
	}
	if !s.started.IsZero() {
		st.Seconds = end.Sub(s.started).Seconds()
		if st.Seconds > 1 {
			st.WPM = float64(correct) / 5 / (st.Seconds / 60)
		}
	}
	st.Accuracy = 100
	if correct+s.errors > 0 {
		st.Accuracy = 100 * float64(correct) / float64(correct+s.errors)
	}
	r.Stats = st
	r.Done = done
	r.ComputeUS = time.Since(t0).Microseconds()
	return r
}
