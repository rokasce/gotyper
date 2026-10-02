package protocol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/rokasce/gotyper/engine/check"
	"github.com/rokasce/gotyper/engine/judge"
	"github.com/rokasce/gotyper/engine/lesson"
)

// Server holds the engine's state between requests: the lessons it can
// serve, whether the handshake has happened and the session for the step
// being played. One Server serves one front end.
type Server struct {
	lib     lesson.Library
	gocache string // GOCACHE for checks; see check.Run
	greeted bool
	session *judge.Session // nil until the first start

	// background counts the goroutines running checks and pre-warms, so
	// Serve can wait for them before it returns.
	background sync.WaitGroup
	// ctx is the context given to NewServer. Cancelling it stops every
	// check and pre-warm still running and makes Serve return.
	ctx context.Context
	// warmCtx is cancelled by stopWarm when the input ends, which stops a
	// pre-warm that is still running: nobody is left to benefit from it.
	warmCtx  context.Context
	stopWarm context.CancelFunc
}

// NewServer returns a server that offers the steps in lib and runs checks
// with gocache as the go build cache (empty: the go command's default).
// Cancelling ctx stops the server: running checks and pre-warms are killed
// (their temporary directories are still removed) and Serve returns. The
// engine cancels it when it is told to stop with SIGTERM or an interrupt.
func NewServer(ctx context.Context, lib lesson.Library, gocache string) *Server {
	warmCtx, stopWarm := context.WithCancel(ctx)
	return &Server{lib: lib, gocache: gocache, ctx: ctx, warmCtx: warmCtx, stopWarm: stopWarm}
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
// the input ends, Serve stops any pre-warm and waits for the checks still
// running, so every check is answered before it returns.
//
// When the server's context is cancelled, Serve returns nil at once without
// waiting for more input; the checks still running are killed, and Serve
// waits for them to clean up.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false) // keep < > & readable; this isn't HTML
	out := &lineWriter{enc: enc}
	defer func() {
		s.stopWarm()
		s.background.Wait()
	}()
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
// tests a buffer. Handle itself runs a check before returning; Serve is what
// runs checks in the background.
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
		s.prewarm(step)
		return s.started(req)
	case OpUpdate:
		if s.session == nil {
			return errorResponse(req, CodeNoStep, "no step in progress; send start first")
		}
		render := s.session.Update(req.Lines, req.Keys, req.Cursor)
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
// handling requests: the step being played and the lines to check. It
// returns a function that runs the check and builds the response. That
// function touches no Server state, so it is safe to run on another
// goroutine while the server goes on to the next request, even if that
// request starts a different step.
func (s *Server) checkJob(req Request) func() Response {
	ctx, step, gocache := s.ctx, s.session.Step(), s.gocache
	source := strings.Join(req.Lines, "\n") + "\n" // a file ends in a newline
	return func() Response {
		res := check.Run(ctx, step, source, gocache)
		return Response{ID: req.ID, Op: req.Op, Check: &res}
	}
}

// prewarm checks the step's own target in the background and throws the
// result away. The point is the side effect: the go command compiles the
// packages the step imports (net/http and the rest) into the build cache, so
// the learner's first check takes about a second instead of several. It is
// stopped when the input ends.
func (s *Server) prewarm(step lesson.Step) {
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		check.Run(s.warmCtx, step, step.Source(), s.gocache)
	}()
}

// started answers start and restart alike: the step's layout plus the render
// of an empty buffer, so the front end can paint the fresh attempt straight
// away without a separate update.
func (s *Server) started(req Request) Response {
	render := s.session.Update([]string{""}, 0, [2]int{})
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
