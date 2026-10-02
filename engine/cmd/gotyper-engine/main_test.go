package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelftestPasses(t *testing.T) {
	lib, err := loadLessons("../../../lessons")
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := selftest(&out, lib, filepath.Join(t.TempDir(), "stats.jsonl")); err != nil {
		t.Fatalf("selftest: %v", err)
	}
	if !strings.Contains(out.String(), "ok") {
		t.Fatalf("summary = %q", out.String())
	}
	t.Log("\n" + out.String())
}

func TestBadFlagsExitNonZero(t *testing.T) {
	if code := run([]string{"--nope"}); code == 0 {
		t.Fatal("unknown flag should fail")
	}
	if code := run([]string{"--selftest", "extra"}); code == 0 {
		t.Fatal("extra arguments should fail")
	}
	if code := run([]string{"--lessons", t.TempDir(), "--selftest"}); code == 0 {
		t.Fatal("an empty lessons directory should fail")
	}
}

// TestFindsLessonsBesideExecutable builds the engine into <tmp>/bin, as the
// Neovim plugin does, and runs it from an unrelated working directory without
// --lessons: directly, through a symlink in another directory, and with no
// lessons beside it.
func TestFindsLessonsBesideExecutable(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the engine binary; skipped in -short mode")
	}
	// EvalSymlinks because the engine resolves its own path, and a temp
	// directory can sit behind a symlink (as /var does on macOS).
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(repo, "bin", "gotyper-engine")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	elsewhere := t.TempDir()
	selftest := func(path string) (string, error) {
		cmd := exec.Command(path, "--selftest")
		cmd.Dir = elsewhere
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	var lessons string
	if out, err := selftest(bin); err == nil {
		t.Fatalf("with no lessons beside the binary, the engine should fail:\n%s", out)
	} else if want := filepath.Join(repo, "lessons"); !strings.Contains(out, want) || !strings.Contains(out, "--lessons") {
		t.Fatalf("error should name %s and suggest --lessons:\n%s", want, out)
	}

	lessons, err = filepath.Abs("../../../lessons")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(lessons, filepath.Join(repo, "lessons")); err != nil {
		t.Fatal(err)
	}
	if out, err := selftest(bin); err != nil {
		t.Fatalf("selftest from %s: %v\n%s", elsewhere, err, out)
	}

	link := filepath.Join(t.TempDir(), "gotyper-engine")
	if err := os.Symlink(bin, link); err != nil {
		t.Fatal(err)
	}
	if out, err := selftest(link); err != nil {
		t.Fatalf("selftest through symlink %s: %v\n%s", link, err, out)
	}
}
