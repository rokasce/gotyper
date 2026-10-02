// Command gotyper-engine is the judging half of gotyper. The Neovim plugin
// starts it as a job and talks to it over stdin/stdout using the NDJSON
// protocol described in engine/PROTOCOL.md.
//
// Usage:
//
//	gotyper-engine              serve the protocol on stdin/stdout
//	gotyper-engine --selftest   play the built-in step in-process and report
//	gotyper-engine --version    print the engine and protocol versions
package main

import (
	"fmt"
	"os"

	"github.com/rokasce/gotyper/engine/lesson"
	"github.com/rokasce/gotyper/engine/protocol"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run is main without the os.Exit, returning the exit code instead.
func run(args []string) int {
	if len(args) > 1 {
		fmt.Fprintln(os.Stderr, "usage: gotyper-engine [--selftest | --version]")
		return 2
	}
	if len(args) == 1 {
		switch args[0] {
		case "--selftest":
			if err := selftest(os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, "selftest FAILED:", err)
				return 1
			}
			return 0
		case "--version":
			fmt.Printf("gotyper-engine %s (protocol %d)\n", protocol.EngineVersion, protocol.Version)
			return 0
		default:
			fmt.Fprintf(os.Stderr, "unknown flag %q\nusage: gotyper-engine [--selftest | --version]\n", args[0])
			return 2
		}
	}
	// Normal mode. Anything written to stdout must be a protocol line, so
	// diagnostics go to stderr only.
	if err := protocol.NewServer(lesson.Fixture()).Serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "gotyper-engine:", err)
		return 1
	}
	return 0
}
