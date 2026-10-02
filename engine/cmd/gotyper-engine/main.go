// Command gotyper-engine is the judging half of gotyper. The Neovim plugin
// starts it as a job and talks to it over stdin/stdout using the NDJSON
// protocol described in engine/PROTOCOL.md.
//
// Usage:
//
//	gotyper-engine [--lessons DIR]              serve the protocol on stdin/stdout
//	gotyper-engine [--lessons DIR] --selftest   play the first step in-process and report
//
// DIR is the lessons directory. Without --lessons the engine looks for a
// directory named "lessons" in the working directory and then in each parent,
// which finds the repository's lessons when run from inside the repository.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/rokasce/gotyper/engine/lesson"
	"github.com/rokasce/gotyper/engine/protocol"
)

const usage = "usage: gotyper-engine [--lessons DIR] [--selftest]"

func main() {
	os.Exit(run(os.Args[1:]))
}

// run is main without the os.Exit, returning the exit code instead.
func run(args []string) int {
	// A FlagSet of our own (rather than the global flag functions) lets
	// tests call run with any arguments. The flag package accepts both
	// -selftest and --selftest.
	flags := flag.NewFlagSet("gotyper-engine", flag.ContinueOnError)
	flags.SetOutput(io.Discard) // we print our own usage line below
	lessonsDir := flags.String("lessons", "", "lessons directory")
	selftestFlag := flags.Bool("selftest", false, "play the first step in-process and report")
	if err := flags.Parse(args); err != nil || flags.NArg() > 0 {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}

	lib, err := loadLessons(*lessonsDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gotyper-engine: loading lessons:", err)
		return 1
	}

	if *selftestFlag {
		if err := selftest(os.Stdout, lib); err != nil {
			fmt.Fprintln(os.Stderr, "selftest FAILED:", err)
			return 1
		}
		return 0
	}
	// Normal mode. Anything written to stdout must be a protocol line, so
	// diagnostics go to stderr only.
	if err := protocol.NewServer(lib).Serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "gotyper-engine:", err)
		return 1
	}
	return 0
}

// loadLessons loads the lessons directory dir, or finds one with
// lesson.FindRoot when dir is empty. A broken lesson stops the engine before
// it serves anything, so the problem is seen at once rather than mid-game.
func loadLessons(dir string) (lesson.Library, error) {
	if dir == "" {
		found, err := lesson.FindRoot(".")
		if err != nil {
			return lesson.Library{}, fmt.Errorf("%w (pass --lessons DIR)", err)
		}
		dir = found
	}
	return lesson.Load(os.DirFS(dir))
}
