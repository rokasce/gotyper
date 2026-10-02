// Package check grades Go code the way the go command does: it builds the
// step's module in a temporary directory with a given version of the step's
// file, then runs go vet and go test on it.
//
// The engine uses it for the check op, which grades what the learner typed.
// TestLessonsOnDisk uses
// it to make sure every lesson's own target passes, so a lesson is checked in
// exactly the way the learner's code will be.
package check

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/rokasce/gotyper/engine/lesson"
)

// The stages of a check, in the order they run. Result.Stage names one.
const (
	// StageSetup is everything before the go command runs: finding go and
	// writing the module to a temporary directory.
	StageSetup = "setup"
	// StageVet is go vet ./..., which also catches compile errors.
	StageVet = "vet"
	// StageTest is go test ./..., which runs the step's hidden tests.
	StageTest = "test"
)

// Timeout bounds each go command. Learner code can loop forever, and a
// check that never answers would leave the learner waiting with no result.
const Timeout = time.Minute

// TestTimeout is go test's own -timeout, kept below Timeout. When a test
// runs too long, the test binary then panics and exits by itself, with a
// stack trace showing where it was stuck. Stopping the go command would not
// stop the test binary it started.
const TestTimeout = 50 * time.Second

// MaxOutputLines is how many lines of go output a Result keeps. Compiler and
// test failures put the useful part first; the rest would not fit beside the
// code anyway.
const MaxOutputLines = 20

// StopGrace is how long a go command that is told to stop (its check was
// cancelled or timed out) gets to stop by itself before it is killed. It is
// well under the 2 seconds Neovim's jobstop waits before killing the engine,
// so the check still has time to remove its temporary directory.
const StopGrace = time.Second

// Result is the outcome of one check.
type Result struct {
	// OK is true when both go vet and go test passed.
	OK bool `json:"ok"`
	// Stage is where the check stopped: the stage that failed, or
	// StageTest when everything passed.
	Stage string `json:"stage"`
	// Output is what the go command printed for that stage, with the
	// temporary directory's path removed and cut to MaxOutputLines.
	Output string `json:"output"`
	// MS is how long the check took, in milliseconds.
	MS int64 `json:"ms"`
}

// Run checks source as the contents of step's file. It writes the step's
// module (carried files, source, hidden files; see lesson.Step.Module) into
// a fresh temporary directory, runs go vet ./... and then go test ./...
// there, and removes the directory again.
//
// The go command uses its usual build cache, which is what makes a second
// check fast: packages such as net/http are compiled once and reused.
// Cancelling ctx stops the go command and ends the check early.
func Run(ctx context.Context, step lesson.Step, source string) Result {
	start := time.Now()
	res := run(ctx, step, source)
	res.MS = time.Since(start).Milliseconds()
	return res
}

func run(ctx context.Context, step lesson.Step, source string) Result {
	gobin, err := exec.LookPath("go")
	if err != nil {
		return Result{Stage: StageSetup, Output: "the go command was not found on PATH: " + err.Error()}
	}
	dir, err := os.MkdirTemp("", "gotyper-check-")
	if err != nil {
		return Result{Stage: StageSetup, Output: err.Error()}
	}
	defer os.RemoveAll(dir)
	if err := step.WriteModule(dir, source); err != nil {
		return Result{Stage: StageSetup, Output: err.Error()}
	}

	// GOWORK=off keeps a go.work file in a parent directory from pulling
	// the module into another workspace. GOFLAGS is cleared so the
	// caller's own flags (such as -short or -mod) do not change the result.
	env := append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	var out []byte
	for _, stage := range []string{StageVet, StageTest} {
		cmdCtx, cancel := context.WithTimeout(ctx, Timeout)
		args := []string{stage, "./..."}
		if stage == StageTest {
			args = []string{stage, "-timeout=" + TestTimeout.String(), "./..."}
		}
		cmd := exec.CommandContext(cmdCtx, gobin, args...)
		cmd.Dir = dir
		cmd.Env = env
		// Stopping the go command with an interrupt rather than a kill lets
		// it remove its own temporary work directory. It does not pass the
		// interrupt on to a test binary it is running; that one ends at
		// TestTimeout. If the go command has not exited after StopGrace, it
		// is killed.
		cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
		cmd.WaitDelay = StopGrace
		out, err = cmd.CombinedOutput()
		timedOut := cmdCtx.Err() == context.DeadlineExceeded
		cancel()
		if err != nil {
			msg := tidy(string(out), dir)
			if timedOut {
				msg = fmt.Sprintf("go %s took longer than %v and was stopped; is there an endless loop?\n%s", stage, Timeout, msg)
			}
			return Result{Stage: stage, Output: msg}
		}
	}
	return Result{OK: true, Stage: StageTest, Output: tidy(string(out), dir)}
}

// tidy makes go output fit for a small panel: the temporary directory's path
// is removed so file names read as they do in the step, surrounding blank
// lines go, and only the first MaxOutputLines lines are kept.
func tidy(out, dir string) string {
	out = strings.ReplaceAll(out, dir+string(os.PathSeparator), "")
	out = strings.ReplaceAll(out, dir, ".")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > MaxOutputLines {
		more := len(lines) - MaxOutputLines
		lines = append(lines[:MaxOutputLines], fmt.Sprintf("... and %d more lines", more))
	}
	return strings.Join(lines, "\n")
}
