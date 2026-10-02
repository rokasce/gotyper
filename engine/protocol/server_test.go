package protocol

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rokasce/gotyper/engine/lesson"
)

// lessons is the repository's lessons directory, loaded once. The tests
// play its first step, json-api/01-greet-handler (28 lines of handler.go).
var lessons = func() lesson.Library {
	lib, err := lesson.Load(os.DirFS("../../lessons"))
	if err != nil {
		panic(err)
	}
	return lib
}()

const (
	firstStep  = "json-api/01-greet-handler"
	recallStep = "json-api/02-greet-handler-recall"
)

// serve feeds input to a fresh server and returns the decoded responses, one
// per output line.
func serve(t *testing.T, input string) []Response {
	t.Helper()
	var out strings.Builder
	if err := NewServer(context.Background(), lessons, "").Serve(strings.NewReader(input), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var resps []Response
	sc := bufio.NewScanner(strings.NewReader(out.String()))
	for sc.Scan() {
		var r Response
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("response line %q is not JSON: %v", sc.Text(), err)
		}
		resps = append(resps, r)
	}
	return resps
}

const hello = `{"id":1,"op":"hello","protocol":1}` + "\n"

func id(r Response) int64 {
	if r.ID == nil {
		return -1
	}
	return *r.ID
}

func TestHandshake(t *testing.T) {
	resps := serve(t, hello)
	if len(resps) != 1 {
		t.Fatalf("got %d responses", len(resps))
	}
	r := resps[0]
	if id(r) != 1 || r.Op != OpHello || r.Error != nil || r.Hello == nil ||
		r.Hello.Protocol != Version || r.Hello.Engine != EngineVersion {
		t.Fatalf("hello response = %+v (hello %+v, error %+v)", r, r.Hello, r.Error)
	}
}

func TestVersionMismatchIsAnErrorNotACrash(t *testing.T) {
	resps := serve(t, `{"id":1,"op":"hello","protocol":99}`+"\n"+
		`{"id":2,"op":"start"}`+"\n"+
		`{"id":3,"op":"hello","protocol":1}`+"\n"+
		`{"id":4,"op":"start"}`+"\n")
	if len(resps) != 4 {
		t.Fatalf("got %d responses, want 4", len(resps))
	}
	if e := resps[0].Error; e == nil || e.Code != CodeVersionMismatch ||
		!strings.Contains(e.Message, "99") || resps[0].Hello == nil {
		t.Fatalf("mismatch response = %+v", resps[0])
	}
	if e := resps[1].Error; e == nil || e.Code != CodeHandshakeRequired {
		t.Fatalf("start after failed hello = %+v", resps[1])
	}
	if resps[2].Error != nil || resps[3].Error != nil || resps[3].Start == nil {
		t.Fatalf("retry after mismatch should work: %+v / %+v", resps[2], resps[3])
	}
}

func TestOpsBeforeHelloAreRefused(t *testing.T) {
	resps := serve(t, `{"id":7,"op":"start"}`+"\n")
	if e := resps[0].Error; id(resps[0]) != 7 || e == nil || e.Code != CodeHandshakeRequired {
		t.Fatalf("response = %+v", resps[0])
	}
}

func TestMalformedLineKeepsEngineRunning(t *testing.T) {
	resps := serve(t, hello+
		"this is not json\n"+
		`{"id":2,"op":"update","lines":5}`+"\n"+
		`{"id":3,"op":"warp"}`+"\n"+
		`{"id":4,"op":"start"}`+"\n")
	if len(resps) != 5 {
		t.Fatalf("got %d responses, want 5", len(resps))
	}
	if e := resps[1].Error; resps[1].ID != nil || e == nil || e.Code != CodeBadJSON {
		t.Fatalf("garbage line: %+v", resps[1])
	}
	if e := resps[2].Error; id(resps[2]) != 2 || e == nil || e.Code != CodeBadJSON {
		t.Fatalf("wrongly typed field: %+v", resps[2])
	}
	if e := resps[3].Error; id(resps[3]) != 3 || e == nil || e.Code != CodeUnknownOp {
		t.Fatalf("unknown op: %+v", resps[3])
	}
	if resps[4].Error != nil || resps[4].Start == nil {
		t.Fatalf("engine should still serve start: %+v", resps[4])
	}
}

func TestUpdateBeforeStart(t *testing.T) {
	resps := serve(t, hello+`{"id":2,"op":"update","lines":["p"]}`+"\n"+`{"id":3,"op":"restart"}`+"\n"+
		`{"id":4,"op":"check","lines":["p"]}`+"\n")
	if len(resps) != 4 {
		t.Fatalf("got %d responses, want 4", len(resps))
	}
	for _, r := range resps[1:] {
		if r.Error == nil || r.Error.Code != CodeNoStep {
			t.Fatalf("%s before start = %+v", r.Op, r)
		}
	}
}

func TestStartUpdateRoundTrip(t *testing.T) {
	resps := serve(t, hello+
		`{"id":2,"op":"start"}`+"\n"+
		`{"id":3,"op":"update","lines":["packx"],"keys":5,"cursor":[0,5]}`+"\n"+
		// The last line has no trailing newline: it must still be answered.
		`{"id":4,"op":"update","lines":["package main"],"keys":13,"cursor":[0,12]}`)
	if len(resps) != 4 {
		t.Fatalf("got %d responses, want 4", len(resps))
	}
	for i, r := range resps {
		if id(r) != int64(i+1) || r.Error != nil {
			t.Fatalf("response %d = %+v (error %+v)", i, r, r.Error)
		}
	}

	st := resps[1].Start
	if st == nil || st.Lines != 28 || len(st.Indents) != 28 || st.Title == "" || st.Width == 0 {
		t.Fatalf("start = %+v", st)
	}
	if r := resps[1].Render; r == nil || len(r.GhostLines) != 27 || len(r.Ghosts) != 1 || r.Ghosts[0].Text != "package main" {
		t.Fatalf("start render = %+v", r)
	}

	typo := resps[2].Render
	if typo == nil || len(typo.ErrorSpans) != 1 || typo.ErrorSpans[0].Col != 4 || typo.Stats.Errors != 1 || typo.Stats.Keys != 5 {
		t.Fatalf("typo render = %+v", typo)
	}
	fixed := resps[3].Render
	if fixed == nil || len(fixed.ErrorSpans) != 0 || fixed.Stats.Errors != 1 || fixed.Stats.Line != 1 || fixed.Done {
		t.Fatalf("fixed render = %+v", fixed)
	}
}

func TestRestartResetsTheAttempt(t *testing.T) {
	resps := serve(t, hello+
		`{"id":2,"op":"start"}`+"\n"+
		`{"id":3,"op":"update","lines":["x"],"keys":1,"cursor":[0,1]}`+"\n"+
		`{"id":4,"op":"restart"}`+"\n"+
		`{"id":5,"op":"update","lines":["p"],"keys":1,"cursor":[0,1]}`+"\n")
	if got := resps[2].Render.Stats.Errors; got != 1 {
		t.Fatalf("errors before restart = %d", got)
	}
	rs := resps[3]
	if rs.Op != OpRestart || rs.Error != nil || rs.Start == nil || rs.Render == nil {
		t.Fatalf("restart response = %+v", rs)
	}
	if rs.Render.Stats.Errors != 0 || rs.Render.Stats.Seconds != 0 || len(rs.Render.ErrorSpans) != 0 {
		t.Fatalf("restart render not fresh: %+v", rs.Render)
	}
	if after := resps[4].Render; after.Stats.Errors != 0 || after.Stats.Accuracy != 100 {
		t.Fatalf("update after restart: %+v", after.Stats)
	}
}

func TestBlankLinesAreIgnored(t *testing.T) {
	if resps := serve(t, "\n  \n"+hello+"\n"); len(resps) != 1 {
		t.Fatalf("got %d responses, want 1", len(resps))
	}
}

func TestListAndStartByID(t *testing.T) {
	resps := serve(t, hello+
		`{"id":2,"op":"list"}`+"\n"+
		`{"id":3,"op":"start","step":"`+firstStep+`"}`+"\n"+
		`{"id":4,"op":"start","step":"json-api/99-nope"}`+"\n"+
		`{"id":5,"op":"update","lines":["p"],"keys":1,"cursor":[0,1]}`+"\n")
	list := resps[1].List
	if resps[1].Error != nil || list == nil || len(list.Tracks) == 0 || list.Tracks[0].ID != "json-api" {
		t.Fatalf("list = %+v (error %+v)", list, resps[1].Error)
	}
	got := list.Tracks[0].Steps[0]
	if got.ID != firstStep || got.Title == "" || got.Mode != lesson.TypeAlong {
		t.Fatalf("first listed step = %+v", got)
	}
	if st := resps[2].Start; resps[2].Error != nil || st == nil || st.Step != firstStep || st.Mode != lesson.TypeAlong {
		t.Fatalf("start by id = %+v (error %+v)", st, resps[2].Error)
	}
	if e := resps[3].Error; e == nil || e.Code != CodeUnknownStep || !strings.Contains(e.Message, "99-nope") {
		t.Fatalf("unknown step = %+v", resps[3])
	}
	// A failed start leaves the step that was already in progress alone.
	if r := resps[4]; r.Error != nil || r.Render == nil {
		t.Fatalf("update after failed start = %+v", r)
	}
}

func TestStartWithoutIDPicksTheFirstStep(t *testing.T) {
	resps := serve(t, hello+`{"id":2,"op":"start"}`+"\n")
	if st := resps[1].Start; st == nil || st.Step != firstStep {
		t.Fatalf("start = %+v", resps[1])
	}
}

func TestStartWithNoLessons(t *testing.T) {
	var out strings.Builder
	in := hello + `{"id":2,"op":"start"}` + "\n" + `{"id":3,"op":"list"}` + "\n"
	if err := NewServer(context.Background(), lesson.Library{}, "").Serve(strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if !strings.Contains(lines[1], CodeUnknownStep) || !strings.Contains(lines[2], `"tracks":[]`) {
		t.Fatalf("responses = %q", lines)
	}
}

// linesJSON encodes lines as a JSON array for a hand-written request.
func linesJSON(t *testing.T, lines []string) string {
	t.Helper()
	b, err := json.Marshal(lines)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestCheckDoesNotBlockUpdates sends a check and then an update. The check
// runs go vet and go test, which takes a second or more, so the update must
// be answered first, and the check's answer must still arrive.
func TestCheckDoesNotBlockUpdates(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go vet and go test")
	}
	step, _ := lessons.Step(firstStep)
	resps := serve(t, hello+
		`{"id":2,"op":"start","step":"`+firstStep+`"}`+"\n"+
		`{"id":3,"op":"check","lines":`+linesJSON(t, step.Target)+`}`+"\n"+
		`{"id":4,"op":"update","lines":["p"],"keys":1,"cursor":[0,1]}`+"\n")
	if len(resps) != 4 {
		t.Fatalf("got %d responses, want 4", len(resps))
	}
	if id(resps[2]) != 4 || resps[2].Render == nil {
		t.Fatalf("the update should be answered before the check, got %+v first", resps[2])
	}
	c := resps[3]
	if id(c) != 3 || c.Op != OpCheck || c.Error != nil || c.Check == nil || !c.Check.OK {
		t.Fatalf("check of the target = %+v (check %+v)", c, c.Check)
	}
}

// TestRecallStep plays the recall step: no ghosts or red for any text, and a
// check grades what was typed. Here it is the target without the return
// after http.Error, which compiles but fails the hidden test.
func TestRecallStep(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go vet and go test")
	}
	step, ok := lessons.Step(recallStep)
	if !ok {
		t.Fatalf("%s is missing", recallStep)
	}
	var forgot []string
	for _, l := range step.Target {
		if strings.TrimSpace(l) != "return" {
			forgot = append(forgot, l)
		}
	}
	resps := serve(t, hello+
		`{"id":2,"op":"start","step":"`+recallStep+`"}`+"\n"+
		`{"id":3,"op":"update","lines":["packx"],"keys":5,"cursor":[0,5]}`+"\n"+
		`{"id":4,"op":"check","lines":`+linesJSON(t, forgot)+`}`+"\n")
	if len(resps) != 4 {
		t.Fatalf("got %d responses, want 4", len(resps))
	}
	st, r := resps[1].Start, resps[1].Render
	if st == nil || st.Mode != lesson.Recall || r == nil || len(r.Ghosts) != 0 || len(r.GhostLines) != 0 {
		t.Fatalf("recall start = %+v, render %+v", st, r)
	}
	if r := resps[2].Render; r == nil || len(r.ErrorSpans) != 0 || len(r.Ghosts) != 0 || r.Stats.Keys != 5 || r.Done {
		t.Fatalf("recall update = %+v", r)
	}
	if c := resps[3].Check; c == nil || c.OK || c.Stage != "test" || !strings.Contains(c.Output, "did you return?") {
		t.Fatalf("check without the return = %+v", c)
	}
}

// TestCancelStopsChecksAndCleansUp cancels the server's context while a
// check runs and the input is still open, as SIGTERM does in the engine.
// Serve must return without waiting for more input, and the check's
// temporary module must be removed.
func TestCancelStopsChecksAndCleansUp(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go vet and go test")
	}
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp) // os.MkdirTemp, used by check.Run, creates its directories here
	checkDirs := func() []string {
		dirs, err := filepath.Glob(filepath.Join(tmp, "gotyper-check-*"))
		if err != nil {
			t.Fatal(err)
		}
		return dirs
	}

	step, _ := lessons.Step(firstStep)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in, feed := io.Pipe()
	defer feed.Close()
	served := make(chan error, 1)
	go func() { served <- NewServer(ctx, lessons, t.TempDir()).Serve(in, io.Discard) }()
	go io.WriteString(feed, hello+
		`{"id":2,"op":"start","step":"`+firstStep+`"}`+"\n"+
		`{"id":3,"op":"check","lines":`+linesJSON(t, step.Target)+`}`+"\n")

	deadline := time.Now().Add(30 * time.Second)
	for len(checkDirs()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no check started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Serve did not return after the context was cancelled")
	}
	if dirs := checkDirs(); len(dirs) != 0 {
		t.Fatalf("check directories left behind: %v", dirs)
	}
}
