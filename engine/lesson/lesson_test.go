package lesson

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// file is shorthand for an in-memory file in a fstest.MapFS.
func file(body string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(body)}
}

const meta1 = `{"title":"One","mode":"type-along","file":"a.go","intro":["Type it."]}`

// twoSteps is a small valid track: step 02 carries step 01's target.
func twoSteps() fstest.MapFS {
	return fstest.MapFS{
		"README.md":                       file("top-level files are ignored"),
		"t/01-one/step.json":              file(meta1),
		"t/01-one/a.go":                   file("package main\n\nfunc a() {}\n"),
		"t/01-one/hidden/go.mod":          file("module m\n"),
		"t/02-two/step.json":              file(`{"title":"Two","mode":"type-along","file":"b.go","carry":["01-one"]}`),
		"t/02-two/b.go":                   file("package main\n"),
		"t/02-two/hidden/go.mod":          file("module m\n"),
		"t/02-two/hidden/sub/x_test.go":   file("package sub\n"),
		"t/02-two/hidden/testdata/in.txt": file("data"),
	}
}

func TestLoadValidTrack(t *testing.T) {
	lib, err := Load(twoSteps())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(lib.Tracks) != 1 || lib.Tracks[0].ID != "t" || len(lib.Tracks[0].Steps) != 2 {
		t.Fatalf("tracks = %+v", lib.Tracks)
	}
	one, ok := lib.Step("t/01-one")
	if !ok || one.Title != "One" || one.Mode != TypeAlong || one.File != "a.go" ||
		len(one.Target) != 3 || one.Target[2] != "func a() {}" || one.Intro[0] != "Type it." {
		t.Fatalf("step one = %+v", one)
	}
	if first, _ := lib.First(); first.ID != "t/01-one" {
		t.Fatalf("First = %s", first.ID)
	}

	two, _ := lib.Step("t/02-two")
	want := map[string]string{
		"a.go":            "package main\n\nfunc a() {}\n", // carried from 01-one
		"b.go":            "package main\n",
		"go.mod":          "module m\n",
		"sub/x_test.go":   "package sub\n",
		"testdata/in.txt": "data",
	}
	got := two.Module(two.Source())
	if len(got) != len(want) {
		t.Fatalf("module files = %v", got)
	}
	for name, body := range want {
		if got[name] != body {
			t.Errorf("module file %s = %q, want %q", name, got[name], body)
		}
	}
}

func TestModuleTakesTheTypedSource(t *testing.T) {
	lib, err := Load(twoSteps())
	if err != nil {
		t.Fatal(err)
	}
	two, _ := lib.Step("t/02-two")
	got := two.Module("package main // typed\n")
	if got["b.go"] != "package main // typed\n" || got["a.go"] != "package main\n\nfunc a() {}\n" {
		t.Fatalf("module with typed source = %v", got)
	}
}

func TestLoadRecallStep(t *testing.T) {
	fsys := twoSteps()
	fsys["t/02-two/step.json"] = file(`{"title":"Two","mode":"recall","file":"b.go","carry":["01-one"]}`)
	lib, err := Load(fsys)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if two, _ := lib.Step("t/02-two"); two.Mode != Recall {
		t.Fatalf("mode = %q, want %q", two.Mode, Recall)
	}
}

// withDrill adds a valid drill step, t/03-drill, to fsys.
func withDrill(fsys fstest.MapFS) fstest.MapFS {
	fsys["t/03-drill/step.json"] = file(`{"title":"Drill","mode":"drill","file":"goal.go","start":"start.go","par":7}`)
	fsys["t/03-drill/goal.go"] = file("package main\n\nfunc b() {}\n")
	fsys["t/03-drill/start.go"] = file("package main\n\nfunc a() {}\n")
	return fsys
}

func TestLoadDrillStep(t *testing.T) {
	lib, err := Load(withDrill(twoSteps()))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	d, _ := lib.Step("t/03-drill")
	if d.Mode != Drill || d.Par != 7 || len(d.Start) != 3 || d.Start[2] != "func a() {}" || d.Target[2] != "func b() {}" {
		t.Fatalf("drill = %+v", d)
	}
}

func TestWriteModule(t *testing.T) {
	lib, err := Load(twoSteps())
	if err != nil {
		t.Fatal(err)
	}
	step, _ := lib.Step("t/02-two")
	dir := t.TempDir()
	if err := step.WriteModule(dir, step.Source()); err != nil {
		t.Fatalf("WriteModule: %v", err)
	}
	for name, want := range step.Module(step.Source()) {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil || string(got) != want {
			t.Errorf("%s on disk = %q, %v; want %q", name, got, err, want)
		}
	}
}

func TestLoadEmptyLessonsIsAnError(t *testing.T) {
	if _, err := Load(fstest.MapFS{"README.md": file("")}); err == nil {
		t.Fatal("a lessons directory without tracks should be an error")
	}
}

// TestLoadErrors checks that each kind of broken lesson is reported with the
// step it is in and a message that says what is wrong.
func TestLoadErrors(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(fstest.MapFS)
		wants []string // substrings the error must contain
	}{
		{"missing step.json", func(f fstest.MapFS) {
			delete(f, "t/01-one/step.json")
		}, []string{"step t/01-one: missing step.json"}},
		{"bad json", func(f fstest.MapFS) {
			f["t/01-one/step.json"] = file(`{"title":`)
		}, []string{"step t/01-one: step.json:"}},
		{"misspelled field", func(f fstest.MapFS) {
			f["t/01-one/step.json"] = file(`{"title":"One","mode":"type-along","file":"a.go","intor":[]}`)
		}, []string{`unknown field "intor"`}},
		{"unknown mode", func(f fstest.MapFS) {
			f["t/01-one/step.json"] = file(`{"title":"One","mode":"race","file":"a.go"}`)
		}, []string{`step t/01-one: step.json: unknown mode "race"`}},
		{"missing mode", func(f fstest.MapFS) {
			f["t/01-one/step.json"] = file(`{"title":"One","file":"a.go"}`)
		}, []string{"mode is missing"}},
		{"missing title", func(f fstest.MapFS) {
			f["t/01-one/step.json"] = file(`{"mode":"type-along","file":"a.go"}`)
		}, []string{"title is empty"}},
		{"missing target", func(f fstest.MapFS) {
			delete(f, "t/01-one/a.go")
		}, []string{"step t/01-one: target file a.go is missing"}},
		{"target in a subdirectory", func(f fstest.MapFS) {
			f["t/01-one/step.json"] = file(`{"title":"One","mode":"type-along","file":"hidden/go.mod"}`)
		}, []string{"must be a plain file name"}},
		{"stray file", func(f fstest.MapFS) {
			f["t/01-one/main_test.go"] = file("package main\n")
		}, []string{"step t/01-one: unexpected main_test.go"}},
		{"carry from a later step", func(f fstest.MapFS) {
			f["t/01-one/step.json"] = file(`{"title":"One","mode":"type-along","file":"a.go","carry":["02-two"]}`)
		}, []string{`step t/01-one: carry "02-two": no earlier step`}},
		{"carry from a missing step", func(f fstest.MapFS) {
			f["t/02-two/step.json"] = file(`{"title":"Two","mode":"type-along","file":"b.go","carry":["01-nope"]}`)
		}, []string{`step t/02-two: carry "01-nope": no earlier step`}},
		{"carry from a broken step", func(f fstest.MapFS) {
			delete(f, "t/01-one/a.go")
		}, []string{`step t/02-two: carry "01-one": that step has errors`}},
		{"carry clashes with the target", func(f fstest.MapFS) {
			f["t/02-two/step.json"] = file(`{"title":"Two","mode":"type-along","file":"a.go","carry":["01-one"]}`)
			f["t/02-two/a.go"] = file("package main\n")
			delete(f, "t/02-two/b.go")
		}, []string{`carry "01-one": its file a.go clashes with the target`}},
		{"hidden file clashes with the target", func(f fstest.MapFS) {
			f["t/01-one/hidden/a.go"] = file("package main\n")
		}, []string{"hidden file a.go clashes with the target"}},
		{"bad step directory name", func(f fstest.MapFS) {
			f["t/three/step.json"] = file(meta1)
		}, []string{"step t/three: directory name must be NN-slug"}},
		{"drill without a start file", func(f fstest.MapFS) {
			f["t/03-drill/step.json"] = file(`{"title":"Drill","mode":"drill","file":"goal.go","par":7}`)
		}, []string{"step t/03-drill: step.json: start is missing"}},
		{"drill start file missing", func(f fstest.MapFS) {
			delete(f, "t/03-drill/start.go")
		}, []string{"step t/03-drill: start file start.go is missing"}},
		{"drill without a par", func(f fstest.MapFS) {
			f["t/03-drill/step.json"] = file(`{"title":"Drill","mode":"drill","file":"goal.go","start":"start.go"}`)
		}, []string{"step t/03-drill: step.json: par must be a keystroke count above 0"}},
		{"drill with hidden files", func(f fstest.MapFS) {
			f["t/03-drill/hidden/go.mod"] = file("module m\n")
		}, []string{"step t/03-drill: unexpected hidden"}},
		{"par on a type-along step", func(f fstest.MapFS) {
			f["t/01-one/step.json"] = file(`{"title":"One","mode":"type-along","file":"a.go","par":3}`)
		}, []string{"step t/01-one: step.json: start and par are for drills only"}},
		{"duplicate step number", func(f fstest.MapFS) {
			f["t/01-again/step.json"] = file(meta1)
		}, []string{"number 01 is already used"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fsys := withDrill(twoSteps())
			c.edit(fsys)
			_, err := Load(fsys)
			if err == nil {
				t.Fatal("Load succeeded, want an error")
			}
			for _, want := range c.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q\ndoes not contain %q", err, want)
				}
			}
		})
	}
}

func TestLoadReportsEveryProblem(t *testing.T) {
	fsys := twoSteps()
	delete(fsys, "t/01-one/a.go")
	fsys["t/02-two/step.json"] = file(`{"title":"Two","mode":"race","file":"b.go"}`)
	_, err := Load(fsys)
	if err == nil || !strings.Contains(err.Error(), "t/01-one") || !strings.Contains(err.Error(), "t/02-two") {
		t.Fatalf("want errors for both steps, got %v", err)
	}
}
