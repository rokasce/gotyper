package protocol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rokasce/gotyper/engine/check"
	"github.com/rokasce/gotyper/engine/judge"
	"github.com/rokasce/gotyper/engine/lesson"
	"github.com/rokasce/gotyper/engine/stats"
)

// Server holds the engine's state between requests: the lessons it can
// serve, whether the handshake has happened and the session for the step
// being played. One Server serves one front end.
type Server struct {
	lib     lesson.Library
	greeted bool
	session *judge.Session // nil until the first start
	// last is the latest render sent for the session, so a check knows
	// whether a type-along attempt was done and with which stats.
	last judge.Render
	// statsPath is the stats file that completed steps are added to.
	statsPath string

	// mu guards attempt and lines, which a check's goroutine reads when it
	// finishes to decide whether the attempt it checked is still going.
	mu sync.Mutex
	// attempt counts start and restart requests, so each attempt has its
	// own number.
	attempt int
	// lines is the buffer from the latest update in this attempt.
	lines []string

	// background counts the goroutines running checks, so Serve can wait
	// for them before it returns.
	background sync.WaitGroup
	// ctx is the context given to NewServer. Cancelling it stops every
	// check still running and makes Serve return.
	ctx context.Context
}

// NewServer returns a server that offers the steps in lib and adds every
// completed step to the stats file at statsPath (see package stats).
// Cancelling ctx stops the server: running checks are killed (their
// temporary directories are still removed) and Serve returns. The engine
// cancels it when it is told to stop with SIGTERM or an interrupt.
func NewServer(ctx context.Context, lib lesson.Library, statsPath string) *Server {
	return &Server{lib: lib, ctx: ctx, statsPath: statsPath}
}

// readResult is one ReadBytes call's result, passed from Serve's reading
// goroutine to its loop.
type readResult struct {
	line []byte
	err  error
}

// lineWriter writes response lines. Checks answer from their own goroutines,
// so the mutex makes sure two responses are never written over each other.
// The first write error is kept for Serve to report.
type lineWriter struct {
	mu  sync.Mutex
	enc *json.Encoder
	err error
}

// send writes resp as one line, unless an earlier write already failed.
func (lw *lineWriter) send(resp Response) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	if lw.err == nil {
		lw.err = lw.enc.Encode(resp) // Encode writes the value followed by "\n"
	}
}

// failed returns the first write error, or nil.
func (lw *lineWriter) failed() error {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.err
}

// Serve reads requests from r and writes responses to w until r reaches end
// of file, which is how the front end says goodbye (Neovim closes the job's
// stdin when the job is stopped). It returns nil on a clean end of input and
// an error only if reading or writing itself fails.
//
// The read loop, in plain words: read bytes up to the next newline; that is
// one request. Decode it, hand it to Handle, encode the response as one line,
// and go round again. A line that isn't valid JSON gets an error response and
// the loop simply continues, so one bad message never kills the engine.
//
// The exception is check, which runs go vet and go test and takes a second
// or more. Serve runs it on a goroutine of its own and goes straight on to
// the next request, so updates typed meanwhile are answered at once. That is
// why a check's answer can come after the answers to later requests. When
// the input ends, Serve waits for the checks still running, so every check
// is answered before it returns.
//
// When the server's context is cancelled, Serve returns nil at once without
// waiting for more input; the checks still running are killed, and Serve
// waits for them to clean up.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false) // keep < > & readable; this isn't HTML
	out := &lineWriter{enc: enc}
	defer s.background.Wait()
	// Reading blocks until a line arrives, and a cancelled context cannot
	// interrupt a read. So reading happens on a goroutine of its own, and
	// the loop below waits for either the next line or the cancellation.
	reads := make(chan readResult)
	go func() {
		in := bufio.NewReader(r)
		for {
			// ReadBytes (rather than bufio.Scanner) has no line-length
			// limit, so even a huge buffer arrives as one request.
			line, err := in.ReadBytes('\n')
			select {
			case reads <- readResult{line, err}:
			case <-s.ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	for {
		var line []byte
		var readErr error
		select {
		case rr := <-reads:
			line, readErr = rr.line, rr.err
		case <-s.ctx.Done():
			return nil
		}
		if len(bytes.TrimSpace(line)) > 0 {
			s.handleLine(line, out.send)
			if err := out.failed(); err != nil {
				return fmt.Errorf("write response: %w", err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			// The last line may lack its "\n"; it was handled above.
			// The deferred wait lets running checks write their answers.
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("read request: %w", readErr)
		}
	}
}

// handleLine decodes one raw request line, handles it and passes the
// response to send.
func (s *Server) handleLine(line []byte, send func(Response)) {
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		// Broken JSON, or a field of the wrong type. Whatever Unmarshal
		// managed to fill in is kept, so the id is echoed when it was
		// readable and is null when it wasn't.
		send(errorResponse(req, CodeBadJSON, "could not decode request: "+err.Error()))
		return
	}
	// A check that can run goes to the background (see Serve). One that
	// will be refused (no hello yet, no step) goes through Handle like any
	// other request and is answered in order.
	if req.Op == OpCheck && s.greeted && s.session != nil {
		job := s.checkJob(req)
		s.background.Add(1)
		go func() {
			defer s.background.Done()
			send(job())
		}()
		return
	}
	send(s.Handle(req))
}

// Handle answers one decoded request. It is the whole protocol state machine:
// hello must succeed before anything else, list describes the lessons,
// start (re)creates the session for a step,
// update judges a buffer, restart resets the attempt, check compiles and
// tests a buffer, stats reports the bests from the stats file. Handle itself
// runs a check before returning; Serve is what runs checks in the
// background.
func (s *Server) Handle(req Request) Response {
	if req.Op == OpHello {
		return s.hello(req)
	}
	if !s.greeted {
		return errorResponse(req, CodeHandshakeRequired,
			fmt.Sprintf(`send {"op":"hello","protocol":%d} first`, Version))
	}
	switch req.Op {
	case OpList:
		return Response{ID: req.ID, Op: req.Op, List: listInfo(s.lib)}
	case OpStart:
		step, ok := s.lib.First()
		if req.Step != "" {
			step, ok = s.lib.Step(req.Step)
		}
		if !ok && req.Step == "" {
			return errorResponse(req, CodeUnknownStep, "the engine has no lessons loaded")
		}
		if !ok {
			return errorResponse(req, CodeUnknownStep,
				fmt.Sprintf("no step %q; send list to see the step ids", req.Step))
		}
		s.session = judge.NewSession(step)
		return s.started(req)
	case OpUpdate:
		if s.session == nil {
			return errorResponse(req, CodeNoStep, "no step in progress; send start first")
		}
		s.last = s.session.Update(req.Lines, req.Keys, req.Cursor)
		s.mu.Lock()
		s.lines = req.Lines
		s.mu.Unlock()
		render := s.last // a copy, so the response keeps it after the next update
		return Response{ID: req.ID, Op: req.Op, Render: &render}
	case OpRestart:
		if s.session == nil {
			return errorResponse(req, CodeNoStep, "no step in progress; send start first")
		}
		s.session.Restart()
		return s.started(req)
	case OpCheck:
		if s.session == nil {
			return errorResponse(req, CodeNoStep, "no step in progress; send start first")
		}
		return s.checkJob(req)()
	case OpStats:
		records, err := stats.Load(s.statsPath)
		if err != nil {
			return errorResponse(req, CodeStatsUnreadable, "reading the stats file: "+err.Error())
		}
		return Response{ID: req.ID, Op: req.Op, Stats: &Stats{Steps: stats.Bests(records)}}
	default:
		return errorResponse(req, CodeUnknownOp, fmt.Sprintf("unknown op %q", req.Op))
	}
}

// hello checks the front end's protocol version. On a mismatch the engine
// stays alive and un-greeted, so a corrected hello can still succeed.
func (s *Server) hello(req Request) Response {
	resp := Response{ID: req.ID, Op: req.Op, Hello: &Hello{Protocol: Version, Engine: EngineVersion}}
	if req.Protocol != Version {
		s.greeted = false
		resp.Error = &Error{
			Code: CodeVersionMismatch,
			Message: fmt.Sprintf("front end speaks protocol %d but engine %s speaks protocol %d; update whichever is older",
				req.Protocol, EngineVersion, Version),
		}
		return resp
	}
	s.greeted = true
	return resp
}

// checkJob reads what a check needs from the server now, on the goroutine
// handling requests: the step being played, the lines to check, the attempt
// and its latest stats. It returns a function that runs the check, records
// the step in the stats file if the check completed it, and builds the
// response. Apart from reading attempt and lines under mu, that function
// touches no Server state, so it is safe to run on another goroutine while
// the server goes on to the next request, even if that request starts a
// different step.
//
// A passing check completes a recall step. A type-along step is complete
// when its text matches (the latest render is done), and the passing check
// is what it is recorded with. Either way the attempt must still be going
// when the check finishes, with the buffer still holding the checked lines:
// after a start or restart the attempt was given up, and after an edit the
// learner is no longer at the code that passed (the front end then asks
// them to submit again, see PROTOCOL.md).
func (s *Server) checkJob(req Request) func() Response {
	ctx, step, stat := s.ctx, s.session.Step(), s.last.Stats
	complete := step.Mode == lesson.Recall || s.last.Done
	attempt := s.attempt                           // only this goroutine writes it, so no lock is needed to read it
	source := strings.Join(req.Lines, "\n") + "\n" // a file ends in a newline
	return func() Response {
		res := check.Run(ctx, step, source)
		if res.OK && complete && s.stillAt(attempt, req.Lines) {
			rec := stats.Record{
				Step: step.ID, Mode: step.Mode,
				WPM: stat.WPM, Accuracy: stat.Accuracy, Keys: stat.Keys, Errors: stat.Errors, Seconds: stat.Seconds,
				Check: "pass", CheckMS: res.MS, Time: time.Now(),
			}
			// Losing a record should not cost the learner the check's
			// answer, so the error only goes to stderr, which the plugin
			// shows as a warning.
			if err := stats.Append(s.statsPath, rec); err != nil {
				fmt.Fprintln(os.Stderr, "gotyper-engine: recording stats:", err)
			}
		}
		return Response{ID: req.ID, Op: req.Op, Check: &res}
	}
}

// stillAt reports whether attempt is still the current attempt and the
// latest update's buffer is lines. It may be called from any goroutine.
func (s *Server) stillAt(attempt int, lines []string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempt == attempt && slices.Equal(s.lines, lines)
}

// started answers start and restart alike: the step's layout plus the render
// of an empty buffer, so the front end can paint the fresh attempt straight
// away without a separate update.
func (s *Server) started(req Request) Response {
	s.mu.Lock()
	s.attempt++ // a check of the previous attempt no longer completes it
	s.lines = []string{""}
	s.mu.Unlock()
	s.last = s.session.Update(s.lines, 0, [2]int{})
	render := s.last
	return Response{ID: req.ID, Op: req.Op, Start: startInfo(s.session), Render: &render}
}

func startInfo(sess *judge.Session) *Start {
	step := sess.Step()
	return &Start{
		Step:    step.ID,
		Mode:    step.Mode,
		Title:   step.Title,
		Intro:   step.Intro,
		Indents: sess.Indents(),
		Lines:   len(step.Target),
		Width:   sess.Width(),
	}
}

// listInfo summarises the library for the list op.
func listInfo(lib lesson.Library) *List {
	list := &List{Tracks: []TrackInfo{}} // [] rather than null when empty
	for _, t := range lib.Tracks {
		info := TrackInfo{ID: t.ID, Steps: []StepInfo{}}
		for _, st := range t.Steps {
			info.Steps = append(info.Steps, StepInfo{ID: st.ID, Title: st.Title, Mode: st.Mode})
		}
		list.Tracks = append(list.Tracks, info)
	}
	return list
}

func errorResponse(req Request, code, msg string) Response {
	return Response{ID: req.ID, Op: req.Op, Error: &Error{Code: code, Message: msg}}
}
