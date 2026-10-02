package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/rokasce/gotyper/engine/judge"
	"github.com/rokasce/gotyper/engine/lesson"
)

// Server holds the engine's state between requests: the lessons it can
// serve, whether the handshake has happened and the session for the step
// being played. One Server serves one front end.
type Server struct {
	lib     lesson.Library
	greeted bool
	session *judge.Session // nil until the first start
}

// NewServer returns a server that offers the steps in lib.
func NewServer(lib lesson.Library) *Server {
	return &Server{lib: lib}
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
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	in := bufio.NewReader(r)
	enc := json.NewEncoder(w) // Encode writes the value followed by "\n"
	enc.SetEscapeHTML(false)  // keep < > & readable; this isn't HTML
	for {
		// ReadBytes (rather than bufio.Scanner) has no line-length
		// limit, so even a huge buffer arrives as one request.
		line, readErr := in.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			if err := enc.Encode(s.handleLine(line)); err != nil {
				return fmt.Errorf("write response: %w", err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil // the last line may lack its "\n"; it was handled above
		}
		if readErr != nil {
			return fmt.Errorf("read request: %w", readErr)
		}
	}
}

// handleLine decodes one raw request line and handles it.
func (s *Server) handleLine(line []byte) Response {
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		// Broken JSON, or a field of the wrong type. Whatever Unmarshal
		// managed to fill in is kept, so the id is echoed when it was
		// readable and is null when it wasn't.
		return errorResponse(req, CodeBadJSON, "could not decode request: "+err.Error())
	}
	return s.Handle(req)
}

// Handle answers one decoded request. It is the whole protocol state machine:
// hello must succeed before anything else, list describes the lessons,
// start (re)creates the session for a step,
// update judges a buffer, restart resets the attempt.
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
		render := s.session.Update(req.Lines, req.Keys, req.Cursor)
		return Response{ID: req.ID, Op: req.Op, Render: &render}
	case OpRestart:
		if s.session == nil {
			return errorResponse(req, CodeNoStep, "no step in progress; send start first")
		}
		s.session.Restart()
		return s.started(req)
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
