package protocol

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rokasce/gotyper/engine/lesson"
)

// serve feeds input to a fresh server and returns the decoded responses, one
// per output line.
func serve(t *testing.T, input string) []Response {
	t.Helper()
	var out strings.Builder
	if err := NewServer(lesson.Fixture()).Serve(strings.NewReader(input), &out); err != nil {
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
	resps := serve(t, hello+`{"id":2,"op":"update","lines":["p"]}`+"\n"+`{"id":3,"op":"restart"}`+"\n")
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
