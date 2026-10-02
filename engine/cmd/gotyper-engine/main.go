// Command gotyper-engine is the judging half of gotyper. The Neovim plugin
// starts it as a job and talks to it over stdin/stdout using the NDJSON
// protocol described in engine/PROTOCOL.md.
//
// Usage:
//
//	gotyper-engine [--lessons DIR]              serve the protocol on stdin/stdout
//	gotyper-engine [--lessons DIR] --selftest   play the first step in-process and report
//
// DIR is the lessons directory. Without --lessons the engine reads
// ../lessons next to its own executable (symlinks resolved), which is the
// repository's lessons/ when the binary is built into <repo>/bin/ as the
// Neovim plugin does. go run builds the binary in a temporary directory, so
// pass --lessons ../lessons when running from engine/.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

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
	//
	// Neovim's jobstop sends SIGTERM, which by default ends a Go program on
	// the spot, before any deferred cleanup runs. Catching it (and Ctrl-C)
	// in a context instead lets the server kill running checks and remove
	// their temporary directories before the engine exits.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	if err := protocol.NewServer(ctx, lib).Serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "gotyper-engine:", err)
		return 1
	}
	return 0
}

// loadLessons loads the lessons directory dir, or ../lessons next to the
// executable when dir is empty. A broken lesson stops the engine before it
// serves anything, so the problem is seen at once rather than mid-game.
func loadLessons(dir string) (lesson.Library, error) {
	if dir != "" {
		lib, err := lesson.Load(os.DirFS(dir))
		if err != nil {
			return lesson.Library{}, fmt.Errorf("%s: %w", dir, err)
		}
		return lib, nil
	}
	dir, err := defaultLessonsDir()
	if err != nil {
		return lesson.Library{}, fmt.Errorf("finding the lessons directory: %w (pass --lessons DIR)", err)
	}
	lib, err := lesson.Load(os.DirFS(dir))
	if err != nil {
		return lesson.Library{}, fmt.Errorf("%s: %w (pass --lessons DIR)", dir, err)
	}
	return lib, nil
}

// defaultLessonsDir returns <directory of the executable>/../lessons. The
// symlinks are resolved first, so a binary linked into another directory
// (say ~/.local/bin) still finds the lessons beside the real file.
func defaultLessonsDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "..", "lessons"), nil
}
