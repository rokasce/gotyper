package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/rokasce/gotyper/engine/judge"
	"github.com/rokasce/gotyper/engine/lesson"
	"github.com/rokasce/gotyper/engine/protocol"
)

// selftest plays the built-in step through the real protocol code, in
// process: it writes NDJSON requests into Serve and reads the NDJSON answers
// back, exactly as the plugin would over a pipe. It plays two attempts:
// a perfect one, then (after a restart) one with a typo that gets fixed.
// It writes a short summary to w and returns an error if anything is off.
func selftest(w io.Writer) error {
	step := lesson.Fixture()
	var reqs []protocol.Request
	nextID := int64(0)
	add := func(req protocol.Request) {
		nextID++
		id := nextID // a fresh variable per request, so each keeps its own id
		req.ID = &id
		reqs = append(reqs, req)
	}
	// typeStep appends one update per finished line, as if the learner typed
	// each line and pressed Enter.
	keys := 0
	typeStep := func() {
		for i := range step.Target {
			keys += len(step.Target[i]) + 1
			lines := step.Target[:i+1]
			add(protocol.Request{Op: protocol.OpUpdate, Lines: lines, Keys: keys, Cursor: [2]int{i, len(lines[i])}})
		}
	}

	add(protocol.Request{Op: protocol.OpHello, Protocol: protocol.Version})
	add(protocol.Request{Op: protocol.OpStart})
	typeStep()
	perfectEnd := len(reqs) - 1

	add(protocol.Request{Op: protocol.OpRestart})
	restartAt := len(reqs) - 1
	keys = 0
	// "packx": the x is typed where the g belongs, then deleted.
	add(protocol.Request{Op: protocol.OpUpdate, Lines: []string{"packx"}, Keys: 5, Cursor: [2]int{0, 5}})
	typoAt := len(reqs) - 1
	add(protocol.Request{Op: protocol.OpUpdate, Lines: []string{"pack"}, Keys: 6, Cursor: [2]int{0, 4}})
	keys = 6
	typeStep()
	typoEnd := len(reqs) - 1

	resps, err := roundTrip(reqs)
	if err != nil {
		return err
	}
	for i, r := range resps {
		if r.Error != nil {
			return fmt.Errorf("request %d (%s): %s: %s", i+1, reqs[i].Op, r.Error.Code, r.Error.Message)
		}
		if r.ID == nil || *r.ID != *reqs[i].ID {
			return fmt.Errorf("response %d has the wrong id", i+1)
		}
	}

	hello := resps[0].Hello
	perfect := resps[perfectEnd].Render
	restart := resps[restartAt].Render
	typo := resps[typoAt].Render
	fixed := resps[typoEnd].Render

	checks := []struct {
		ok   bool
		what string
	}{
		{hello != nil && hello.Protocol == protocol.Version, "handshake reports our protocol version"},
		{perfect.Done && perfect.Stats.Errors == 0 && perfect.Stats.Accuracy == 100, "perfect run is done with no errors"},
		{restart.Stats.Errors == 0 && !restart.Done && len(restart.GhostLines) == len(step.Target)-1, "restart starts a fresh attempt"},
		{len(typo.ErrorSpans) == 1 && typo.ErrorSpans[0] == (judge.Span{Row: 0, Col: 4, EndCol: 5}), "typo is highlighted"},
		{fixed.Done && len(fixed.ErrorSpans) == 0 && fixed.Stats.Errors == 1, "fixed typo run is done with one error charged"},
	}
	for _, c := range checks {
		if !c.ok {
			return fmt.Errorf("%s: check failed", c.what)
		}
	}

	fmt.Fprintf(w, "gotyper-engine %s selftest (protocol %d): ok\n", protocol.EngineVersion, protocol.Version)
	fmt.Fprintf(w, "  perfect run: %d/%d lines, %d errors, accuracy %.1f%%\n",
		perfect.Stats.Line, perfect.Stats.Lines, perfect.Stats.Errors, perfect.Stats.Accuracy)
	fmt.Fprintf(w, "  restart:     errors back to %d\n", restart.Stats.Errors)
	fmt.Fprintf(w, "  typo run:    %d/%d lines, %d error, accuracy %.1f%%\n",
		fixed.Stats.Line, fixed.Stats.Lines, fixed.Stats.Errors, fixed.Stats.Accuracy)
	return nil
}

// roundTrip encodes reqs as NDJSON, serves them, and decodes the answers.
func roundTrip(reqs []protocol.Request) ([]protocol.Response, error) {
	var in strings.Builder
	enc := json.NewEncoder(&in)
	for _, r := range reqs {
		if err := enc.Encode(r); err != nil {
			return nil, err
		}
	}
	var out strings.Builder
	srv := protocol.NewServer(lesson.Fixture())
	if err := srv.Serve(strings.NewReader(in.String()), &out); err != nil {
		return nil, err
	}
	var resps []protocol.Response
	sc := bufio.NewScanner(strings.NewReader(out.String()))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var r protocol.Response
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, fmt.Errorf("bad response line %q: %w", sc.Text(), err)
		}
		resps = append(resps, r)
	}
	if len(resps) != len(reqs) {
		return nil, fmt.Errorf("sent %d requests, got %d responses", len(reqs), len(resps))
	}
	return resps, nil
}
