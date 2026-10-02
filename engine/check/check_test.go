package check

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/rokasce/gotyper/engine/lesson"
)

// demoStep is the first step of the repository's lessons: handler.go, whose
// hidden test asks for a 400 with only the error in the body.
var demoStep = func() lesson.Step {
	lib, err := lesson.Load(os.DirFS("../../lessons"))
	if err != nil {
		panic(err)
	}
	step, ok := lib.Step("json-api/01-greet-handler")
	if !ok {
		panic("json-api/01-greet-handler is missing")
	}
	return step
}()

// without returns the target's source with the first line equal to drop
// (indentation ignored) removed.
func without(t *testing.T, drop string) string {
	t.Helper()
	lines := append([]string(nil), demoStep.Target...)
	for i, l := range lines {
		if strings.TrimSpace(l) == drop {
			return strings.Join(append(lines[:i], lines[i+1:]...), "\n") + "\n"
		}
	}
	t.Fatalf("the target has no line %q", drop)
	return ""
}

func TestTargetPasses(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go vet and go test")
	}
	res := Run(context.Background(), demoStep, demoStep.Source())
	if !res.OK || res.Stage != StageTest || !strings.HasPrefix(res.Output, "ok") {
		t.Fatalf("the target should pass: %+v", res)
	}
}

// TestForgottenReturnFailsTheTest is the errors-as-values lesson of step 1:
// without the return after http.Error, the handler goes on to write the JSON
// body too. That still compiles, so only the hidden test can catch it.
func TestForgottenReturnFailsTheTest(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go vet and go test")
	}
	res := Run(context.Background(), demoStep, without(t, "return"))
	if res.OK || res.Stage != StageTest || !strings.Contains(res.Output, "did you return?") {
		t.Fatalf("want a go test failure asking about the return, got %+v", res)
	}
}

func TestCompileErrorFailsVet(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go vet")
	}
	res := Run(context.Background(), demoStep, without(t, `"errors"`))
	if res.OK || res.Stage != StageVet || !strings.Contains(res.Output, "handler.go") {
		t.Fatalf("want a go vet failure naming handler.go, got %+v", res)
	}
	if strings.Contains(res.Output, os.TempDir()) {
		t.Fatalf("output still names the temporary directory:\n%s", res.Output)
	}
}

func TestTidyCutsLongOutput(t *testing.T) {
	long := "/tmp/x/a.go:1: bad\n" + strings.Repeat("line\n", 30)
	got := strings.Split(tidy(long, "/tmp/x"), "\n")
	if len(got) != MaxOutputLines+1 || got[0] != "a.go:1: bad" || got[MaxOutputLines] != "... and 11 more lines" {
		t.Fatalf("tidy = %q", got)
	}
}
