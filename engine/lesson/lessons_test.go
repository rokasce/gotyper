// This file is in package lesson_test, not lesson, because it uses the check
// package, which itself imports lesson. A _test package is compiled as a
// separate package, so the import does not go round in a circle.
package lesson_test

import (
	"context"
	"go/format"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/rokasce/gotyper/engine/check"
	"github.com/rokasce/gotyper/engine/lesson"
)

// TestLessonsOnDisk is how a broken lesson is caught. It loads the
// repository's lessons directory and, for every step, checks the step's own
// target with check.Run: the module (carried files + target + hidden files)
// is assembled in a temporary directory and go vet and go test run on it,
// exactly as the learner's code is checked in the game. A failure is
// reported under the step's ID.
//
// It runs the go command, so it is skipped by go test -short.
func TestLessonsOnDisk(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and tests every lesson step; skipped in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatalf("the go command is needed to check lessons: %v", err)
	}
	const root = "../../lessons"
	lib, err := lesson.Load(os.DirFS(root))
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
				// An empty gocache uses the go command's usual cache.
				if res := check.Run(context.Background(), step, step.Source(), ""); !res.OK {
					t.Fatalf("step %s: go %s failed:\n%s", step.ID, res.Stage, res.Output)
				}
			})
		}
	}
}
