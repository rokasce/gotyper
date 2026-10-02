package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// serve sends one request through the handler that routes builds, the way
// the server would, and returns the recorded response.
func serve(t *testing.T, a *api, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	a.routes().ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestRoutes(t *testing.T) {
	a := &api{store: &Store{}}
	first := a.store.Add(Bookmark{URL: "https://go.dev", Title: "Go"})
	second := a.store.Add(Bookmark{URL: "https://pkg.go.dev"})

	rec := serve(t, a, "GET", "/bookmarks")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /bookmarks: status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("GET /bookmarks: Content-Type = %q, want application/json", ct)
	}
	var list []Bookmark
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("GET /bookmarks: body %q is not a JSON list: %v", rec.Body, err)
	}
	if len(list) != 2 || list[0] != first || list[1] != second {
		t.Fatalf("GET /bookmarks: got %+v, want the store's two bookmarks", list)
	}

	rec = serve(t, a, "GET", "/bookmarks/2")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /bookmarks/2: status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("GET /bookmarks/2: Content-Type = %q, want application/json", ct)
	}
	var got Bookmark
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("GET /bookmarks/2: body %q is not a JSON bookmark: %v", rec.Body, err)
	}
	if got != second {
		t.Fatalf("GET /bookmarks/2: got %+v, want %+v", got, second)
	}

	for _, target := range []string{"/bookmarks/3", "/bookmarks/0"} {
		if rec := serve(t, a, "GET", target); rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s: status = %d, want 404 for a bookmark that doesn't exist", target, rec.Code)
		}
	}
	if rec := serve(t, a, "GET", "/bookmarks/abc"); rec.Code != http.StatusBadRequest {
		t.Fatalf("GET /bookmarks/abc: status = %d, want 400 for an id that isn't a number", rec.Code)
	}
}

// TestRoutesMethods checks that the patterns name their method: the mux
// itself answers 405 for a method no pattern accepts.
func TestRoutesMethods(t *testing.T) {
	a := &api{store: &Store{}}
	a.store.Add(Bookmark{URL: "https://go.dev"})
	for _, req := range [][2]string{{"DELETE", "/bookmarks"}, {"PUT", "/bookmarks/1"}} {
		if rec := serve(t, a, req[0], req[1]); rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s: status = %d, want 405 (does the pattern start with GET?)", req[0], req[1], rec.Code)
		}
	}
}
