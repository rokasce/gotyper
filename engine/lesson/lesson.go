// Package lesson loads the material the learner types from the lessons
// directory on disk and checks that it is well formed.
//
// The lessons directory holds tracks, a track holds numbered steps, and a
// step is one screenful of Go code plus the explanation shown next to it:
//
//	lessons/
//	  json-api/                  a track
//	    01-greet-handler/        a step: NN-slug, played in NN order
//	      step.json              title, intro, mode, the typed file, carry
//	      handler.go             the target: the file the learner types
//	      hidden/                files the learner never sees: go.mod,
//	        go.mod               scaffolding such as main.go, and the
//	        main.go              *_test.go files that grade the step
//	        handler_test.go
//
// A step's Go module is its carried files (targets typed in earlier steps of
// the same track), its own target and its hidden files, all in one
// directory. Step.Module assembles it.
package lesson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// Mode says how a step is played and what completes it.
type Mode string

// The modes a step can declare.
const (
	// TypeAlong steps show the target as ghost text; the step is done when
	// every typed line matches its target line exactly (indentation aside).
	TypeAlong Mode = "type-along"
	// Recall steps hide the target: the learner writes the file from memory
	// and submits it, and the step is done when the submitted file passes
	// go vet and go test. The target is only the reference answer, used to
	// check the lesson itself.
	Recall Mode = "recall"
)

// MetaFile is the name of the metadata file in every step directory.
const MetaFile = "step.json"

// HiddenDir is the name of the step subdirectory holding the hidden files.
const HiddenDir = "hidden"

// Step is one step of a track: a title, a short explanation, the file the
// learner types, and everything else needed to compile and test it.
//
// Target lines keep their real indentation (tabs, as gofmt writes them). The
// judge ignores indentation when comparing, but it still uses these tabs to
// tell the front end how deep each line should be indented.
type Step struct {
	// ID is "<track>/<step directory>", for example
	// "json-api/01-greet-handler". The front end names a step by its ID.
	ID    string
	Title string
	// Intro is the explanation shown beside the code, as lines of
	// markdown.
	Intro []string
	Mode  Mode
	// File is the name the target has in the step's module, such as
	// "handler.go".
	File   string
	Target []string
	// Carried maps a file name to its contents for every target carried
	// from an earlier step of the same track.
	Carried map[string]string
	// Hidden maps a slash-separated path, relative to the module root, to
	// its contents.
	Hidden map[string]string
}

// Source returns the target as file contents: its lines joined with "\n",
// ending in a newline.
func (s Step) Source() string {
	return strings.Join(s.Target, "\n") + "\n"
}

// Module returns every file of the step's Go module, keyed by slash path:
// the carried files, the step's own file and the hidden files. source is the
// contents of the step's own file: Source() to check the lesson, or what the
// learner typed to check their work. Writing these into an empty directory
// gives a module that go vet and go test can run on. The loader has already
// made sure no two of them share a path.
func (s Step) Module(source string) map[string]string {
	files := map[string]string{s.File: source}
	for name, body := range s.Carried {
		files[name] = body
	}
	for name, body := range s.Hidden {
		files[name] = body
	}
	return files
}

// WriteModule writes the files of Module(source) into dir, creating
// subdirectories as needed. dir should be empty, such as a fresh temporary
// directory.
func (s Step) WriteModule(dir, source string) error {
	for name, body := range s.Module(source) {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Track is a named sequence of steps, in the order they are played.
type Track struct {
	// ID is the track's directory name, for example "json-api".
	ID    string
	Steps []Step
}

// Library is every track the engine loaded.
type Library struct {
	Tracks []Track
}

// Step finds a step by its ID.
func (l Library) Step(id string) (Step, bool) {
	for _, t := range l.Tracks {
		for _, s := range t.Steps {
			if s.ID == id {
				return s, true
			}
		}
	}
	return Step{}, false
}

// First returns the first step of the first track, which is where a learner
// who has not picked a step begins.
func (l Library) First() (Step, bool) {
	for _, t := range l.Tracks {
		if len(t.Steps) > 0 {
			return t.Steps[0], true
		}
	}
	return Step{}, false
}

// slug is the shape of a track directory name and of the part of a step
// directory name after its number: lowercase words joined by dashes.
const slug = `[a-z0-9]+(-[a-z0-9]+)*`

var (
	trackName = regexp.MustCompile(`^` + slug + `$`)
	// stepName is "NN-slug". The two digits make directory order (which is
	// alphabetical) the order the steps are played in.
	stepName = regexp.MustCompile(`^[0-9]{2}-` + slug + `$`)
)

// Load reads every track in fsys, whose root is the lessons directory, and
// validates it. Use os.DirFS to load a directory on disk.
//
// Loading does not stop at the first problem: it gathers every problem it
// finds, each naming the track or step it belongs to, and returns them
// joined into one error. Files at the top level, such as a README, are
// ignored.
func Load(fsys fs.FS) (Library, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return Library{}, err
	}
	var lib Library
	var errs []error
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		track, err := loadTrack(fsys, e.Name())
		if err != nil {
			errs = append(errs, err)
			continue
		}
		lib.Tracks = append(lib.Tracks, track)
	}
	if len(lib.Tracks) == 0 && len(errs) == 0 {
		errs = append(errs, errors.New("no track directories found"))
	}
	return lib, errors.Join(errs...)
}

// loadTrack loads the steps in one track directory, in directory order.
func loadTrack(fsys fs.FS, name string) (Track, error) {
	if !trackName.MatchString(name) {
		return Track{}, fmt.Errorf("track %q: directory name must be lowercase words joined by dashes", name)
	}
	entries, err := fs.ReadDir(fsys, name)
	if err != nil {
		return Track{}, fmt.Errorf("track %q: %w", name, err)
	}
	track := Track{ID: name}
	var errs []error
	// loaded holds the steps loaded so far, by directory name, so a step
	// can carry files from the steps before it. failed remembers steps that
	// had errors, so a carry from one of them gets a precise message.
	loaded := map[string]Step{}
	failed := map[string]bool{}
	numbers := map[string]string{} // "01" -> the step directory using it
	for _, e := range entries {
		if !e.IsDir() {
			continue // a track README or similar
		}
		dir := e.Name()
		id := name + "/" + dir
		if !stepName.MatchString(dir) {
			errs = append(errs, fmt.Errorf("step %s: directory name must be NN-slug, such as 01-hello", id))
			continue
		}
		if other, ok := numbers[dir[:2]]; ok {
			errs = append(errs, fmt.Errorf("step %s: number %s is already used by %s", id, dir[:2], other))
			failed[dir] = true
			continue
		}
		numbers[dir[:2]] = dir
		step, err := loadStep(fsys, path.Join(name, dir), id, loaded, failed)
		if err != nil {
			errs = append(errs, err)
			failed[dir] = true
			continue
		}
		loaded[dir] = step
		track.Steps = append(track.Steps, step)
	}
	if len(track.Steps) == 0 && len(errs) == 0 {
		errs = append(errs, fmt.Errorf("track %q has no steps", name))
	}
	return track, errors.Join(errs...)
}

// meta is the JSON shape of step.json.
type meta struct {
	Title string   `json:"title"`
	Intro []string `json:"intro"`
	Mode  Mode     `json:"mode"`
	File  string   `json:"file"`
	// Carry lists earlier steps of the same track, by directory name,
	// whose targets are copied into this step's module.
	Carry []string `json:"carry"`
}

// loadStep reads and validates one step directory. dir is its path inside
// fsys and id its step ID. earlier and failed are the steps before it in the
// track that loaded and that failed to load, for resolving carry.
func loadStep(fsys fs.FS, dir, id string, earlier map[string]Step, failed map[string]bool) (Step, error) {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("step %s: "+format, append([]any{id}, args...)...))
	}

	raw, err := fs.ReadFile(fsys, path.Join(dir, MetaFile))
	if err != nil {
		return Step{}, fmt.Errorf("step %s: missing %s", id, MetaFile)
	}
	var m meta
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields() // a misspelled field is an error, not silently ignored
	if err := dec.Decode(&m); err != nil {
		return Step{}, fmt.Errorf("step %s: %s: %w", id, MetaFile, err)
	}

	if strings.TrimSpace(m.Title) == "" {
		fail("%s: title is empty", MetaFile)
	}
	switch m.Mode {
	case TypeAlong, Recall:
	case "":
		fail("%s: mode is missing; use %q or %q", MetaFile, TypeAlong, Recall)
	default:
		fail("%s: unknown mode %q; use %q or %q", MetaFile, m.Mode, TypeAlong, Recall)
	}

	if m.Intro == nil {
		m.Intro = []string{} // so the protocol sends [] rather than null
	}
	step := Step{ID: id, Title: m.Title, Intro: m.Intro, Mode: m.Mode, File: m.File,
		Carried: map[string]string{}, Hidden: map[string]string{}}
	// from records where each module file came from, to report two sources
	// writing the same path.
	from := map[string]string{}

	switch {
	case m.File == "":
		fail("%s: file is missing; name the file the learner types", MetaFile)
	case m.File != path.Base(m.File) || m.File == MetaFile || m.File == HiddenDir || m.File == "." || m.File == "..":
		fail("%s: file %q must be a plain file name in the step directory", MetaFile, m.File)
	default:
		src, err := fs.ReadFile(fsys, path.Join(dir, m.File))
		switch {
		case err != nil:
			fail("target file %s is missing", m.File)
		case len(bytes.TrimSpace(src)) == 0:
			fail("target file %s is empty", m.File)
		default:
			step.Target = splitLines(string(src))
			from[m.File] = "the target"
		}
	}

	// Anything else in the step directory is a mistake, most likely a file
	// that was meant to go under hidden/ or a target with the wrong name.
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return Step{}, fmt.Errorf("step %s: %w", id, err)
	}
	for _, e := range entries {
		switch name := e.Name(); {
		case name == MetaFile || name == m.File:
		case name == HiddenDir && e.IsDir():
		default:
			fail("unexpected %s; hidden files go under %s/", name, HiddenDir)
		}
	}

	hiddenRoot := path.Join(dir, HiddenDir)
	if info, err := fs.Stat(fsys, hiddenRoot); err == nil && info.IsDir() {
		// WalkDir visits every file under hidden/, subdirectories included,
		// so a lesson can ship packages or testdata/ alongside go.mod.
		err := fs.WalkDir(fsys, hiddenRoot, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			body, err := fs.ReadFile(fsys, p)
			if err != nil {
				return err
			}
			rel := strings.TrimPrefix(p, hiddenRoot+"/")
			if src, dup := from[rel]; dup {
				fail("hidden file %s clashes with %s", rel, src)
				return nil
			}
			from[rel] = "hidden/" + rel
			step.Hidden[rel] = string(body)
			return nil
		})
		if err != nil {
			fail("reading %s/: %v", HiddenDir, err)
		}
	}

	for _, name := range m.Carry {
		prev, ok := earlier[name]
		switch {
		case ok:
		case failed[name]:
			fail("carry %q: that step has errors of its own", name)
			continue
		default:
			fail("carry %q: no earlier step with that directory name in this track", name)
			continue
		}
		if src, dup := from[prev.File]; dup {
			fail("carry %q: its file %s clashes with %s", name, prev.File, src)
			continue
		}
		from[prev.File] = "carry " + name
		step.Carried[prev.File] = prev.Source()
	}

	return step, errors.Join(errs...)
}

// splitLines turns file contents into lines without their "\n". The final
// newline that ends every text file does not start an extra, empty line.
func splitLines(src string) []string {
	return strings.Split(strings.TrimSuffix(src, "\n"), "\n")
}
