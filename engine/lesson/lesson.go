// Package lesson describes the material the learner types: a Step is one
// screenful of Go code plus the explanation shown next to it.
//
// For now the engine knows exactly one step, the embedded Fixture. Loading
// lessons from files on disk comes later and will build on the Step type.
package lesson

import (
	_ "embed" // enables the //go:embed directive below
	"strings"
)

// Step is one type-along step: a title, a short explanation, and the lines
// of Go code the learner types over the ghost text.
//
// Target lines keep their real indentation (tabs, as gofmt writes them). The
// judge ignores indentation when comparing, but it still uses these tabs to
// tell the front end how deep each line should be indented.
type Step struct {
	Title  string
	Intro  []string
	Target []string
}

// handlerSource is the code the fixture step asks the learner to type. It
// lives in a .txt file so the Go toolchain does not try to compile it as part
// of this package; //go:embed copies the file's bytes into the binary at build
// time, so the engine needs no files on disk at run time.
//
//go:embed fixture/handler.go.txt
var handlerSource string

// Fixture returns the single built-in step: a small net/http handler that
// returns errors as values and encodes JSON. It gives the start op something
// to serve until real lessons exist.
func Fixture() Step {
	return Step{
		Title: "Step 1 - a JSON handler with errors as values",
		Intro: []string{
			"Type the ghost text. Mistakes turn red;",
			"fix them with any vim motion.",
			"",
			"greet returns (greeting, error) instead of panicking:",
			"the caller decides what an error means. greetHandler",
			"maps it to HTTP 400, and encodes success as JSON.",
			"",
			"Indentation is inserted for you on <Enter>.",
		},
		Target: splitLines(handlerSource),
	}
}

// splitLines turns file contents into lines without their "\n". The final
// newline that ends every text file does not start an extra, empty line.
func splitLines(src string) []string {
	return strings.Split(strings.TrimSuffix(src, "\n"), "\n")
}
