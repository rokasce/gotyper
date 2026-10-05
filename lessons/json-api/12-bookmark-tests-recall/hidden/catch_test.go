package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// brokenCreates are wrong versions of create. A test of create is only worth
// something if it fails when create is wrong, so the learner's tests must fail
// against each of these.
var brokenCreates = []struct{ what, src string }{
	{"skips the url check, so a bad url is stored with 201", `package main

import (
	"encoding/json"
	"net/http"
	"time"
)

func (a *api) create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL   string ` + "`json:\"url\"`" + `
		Title string ` + "`json:\"title\"`" + `
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		http.Error(w, "bad JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	b := a.store.Add(Bookmark{URL: in.URL, Title: in.Title, Created: time.Now()})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(b)
}
`},
	{"answers 400 to every request, even a good one", `package main

import "net/http"

func (a *api) create(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "no", http.StatusBadRequest)
}
`},
}

// TestYourTestsCatchBrokenCreate copies the module into a temporary directory
// with a broken create.go in place of the real one, and runs go test there.
// Only the learner's tests run in the copy (this file is left out), and they
// must fail. In this module they run against the real create as usual, and
// must pass.
func TestYourTestsCatchBrokenCreate(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, "go.mod")
	for _, broken := range brokenCreates {
		dir := t.TempDir()
		for _, name := range files {
			if name == "create.go" || name == "catch_test.go" {
				continue
			}
			src, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name), src, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "create.go"), []byte(broken.src), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("go", "test")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Errorf("your tests pass even when create %s; they must fail then", broken.what)
		} else if !strings.Contains(string(out), "--- FAIL") {
			t.Fatalf("could not run your tests against a create that %s: %v\n%s", broken.what, err, out)
		}
	}
}
