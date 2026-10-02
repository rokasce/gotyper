package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// post sends body to POST /bookmarks through the handler that routes builds,
// so the test also checks that step 7's routes reach create.
func post(t *testing.T, a *api, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	a.routes().ServeHTTP(rec, httptest.NewRequest("POST", "/bookmarks", strings.NewReader(body)))
	return rec
}

func TestCreateRejectsBadInput(t *testing.T) {
	a := &api{store: &Store{}}
	bad := []struct{ why, body string }{
		{"malformed JSON", `{"url": "https://go.dev"`},
		{"not a JSON object", `"https://go.dev"`},
		{"an unknown field", `{"url": "https://go.dev", "tags": ["go"]}`},
		{"no url", `{"title": "Go"}`},
		{"an empty url", `{"url": ""}`},
		{"a url that is not http or https", `{"url": "ftp://go.dev"}`},
		{"a relative url", `{"url": "/doc"}`},
		{"a url with no host", `{"url": "https:go.dev"}`},
	}
	for _, c := range bad {
		rec := post(t, a, c.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body with %s, %s: status = %d, want 400", c.why, c.body, rec.Code)
		}
		if strings.TrimSpace(rec.Body.String()) == "" {
			t.Fatalf("body with %s, %s: 400 with no message; say what was wrong", c.why, c.body)
		}
	}
	if list := a.store.List(); len(list) != 0 {
		t.Fatalf("after only bad requests the store holds %+v; bad input must not be stored", list)
	}
}

func TestCreate(t *testing.T) {
	a := &api{store: &Store{}}
	a.store.Add(Bookmark{URL: "https://go.dev"})

	rec := post(t, a, `{"url": "https://pkg.go.dev/net/http", "title": "net/http"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var got Bookmark
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %q is not a JSON bookmark: %v", rec.Body, err)
	}
	want := Bookmark{ID: 2, URL: "https://pkg.go.dev/net/http", Title: "net/http"}
	if got != want {
		t.Fatalf("got %+v, want %+v: the bookmark as stored, with its new id", got, want)
	}
	if stored, ok := a.store.Get(2); !ok || stored != want {
		t.Fatalf("store.Get(2) = %+v, %v; want %+v, true", stored, ok, want)
	}

	if rec := post(t, a, `{"url": "http://example.com"}`); rec.Code != http.StatusCreated {
		t.Fatalf("plain http url: status = %d, want 201; body: %s", rec.Code, rec.Body)
	}
}
