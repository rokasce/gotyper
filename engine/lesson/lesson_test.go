package lesson

import (
	"go/format"
	"strings"
	"testing"
)

func TestFixtureIsGofmtedGo(t *testing.T) {
	step := Fixture()
	if len(step.Target) != 28 {
		t.Fatalf("fixture has %d lines, want 28", len(step.Target))
	}
	src := strings.Join(step.Target, "\n") + "\n"
	formatted, err := format.Source([]byte(src))
	if err != nil {
		t.Fatalf("fixture is not valid Go: %v", err)
	}
	if string(formatted) != src {
		t.Fatalf("fixture is not gofmt-formatted; the learner would type non-canonical code")
	}
}
