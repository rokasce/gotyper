package lesson

import (
	"go/format"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestLessonsOnDisk is how a broken lesson is caught. It loads the
// repository's lessons directory and, for every step, assembles the step's
// module in a temporary directory (carried files + target + hidden files) and
// runs go vet and go test on it, exactly as the learner's finished code would
// be checked. A failure is reported under the step's ID.
//
// It runs the go command, so it is skipped by go test -short.
func TestLessonsOnDisk(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and tests every lesson step; skipped in -short mode")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("the go command is needed to check lessons: %v", err)
	}
	const root = "../../lessons"
	lib, err := Load(os.DirFS(root))
	if err != nil {
		t.Fatalf("lessons in %s do not load:\n%v", root, err)
	}

	for _, track := range lib.Tracks {
		for _, step := range track.Steps {
			// The subtest name is the step ID, so a failure reads
			// "TestLessonsOnDisk/json-api/01-greet-handler".
			t.Run(step.ID, func(t *testing.T) {
				t.Parallel()
				if strings.HasSuffix(step.File, ".go") {
					// The learner should type canonical Go.
					if formatted, err := format.Source([]byte(step.Source())); err != nil {
						t.Errorf("target %s is not valid Go: %v", step.File, err)
					} else if string(formatted) != step.Source() {
						t.Errorf("target %s is not gofmt-formatted", step.File)
					}
				}
				dir := t.TempDir()
				if err := step.WriteModule(dir); err != nil {
					t.Fatal(err)
				}
				for _, sub := range []string{"vet", "test"} {
					cmd := exec.Command(gobin, sub, "./...")
					cmd.Dir = dir
					// GOWORK=off keeps a go.work file in a parent
					// directory from pulling the step into another
					// workspace; GOFLAGS is cleared so the caller's
					// flags (such as -short) do not leak in.
					cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("step %s: go %s failed: %v\n%s", step.ID, sub, err, out)
					}
				}
			})
		}
	}
}
