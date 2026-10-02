package main

import (
	"encoding/json"
	"testing"
	"time"
)

var created = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// encode marshals b and decodes it back into a map, so the test sees the
// JSON field names whatever order the struct declares them in.
func encode(t *testing.T, b Bookmark) map[string]any {
	t.Helper()
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

func TestBookmarkFieldNames(t *testing.T) {
	fields := encode(t, Bookmark{ID: 7, URL: "https://go.dev", Title: "Go", Created: created})
	want := map[string]any{
		"id":      float64(7),
		"url":     "https://go.dev",
		"title":   "Go",
		"created": "2026-10-01T12:00:00Z",
	}
	if len(fields) != len(want) {
		t.Fatalf("encoded fields = %v, want %v", fields, want)
	}
	for name, v := range want {
		if fields[name] != v {
			t.Fatalf("field %q = %v, want %v (all fields: %v)", name, fields[name], v, fields)
		}
	}
}

func TestBookmarkOmitsEmptyTitle(t *testing.T) {
	fields := encode(t, Bookmark{ID: 1, URL: "https://go.dev", Created: created})
	if _, ok := fields["title"]; ok {
		t.Fatalf("an empty title was encoded: %v (want omitempty)", fields)
	}
	for _, name := range []string{"id", "url", "created"} {
		if _, ok := fields[name]; !ok {
			t.Fatalf("field %q is missing: %v (only title is omitempty)", name, fields)
		}
	}
}

func TestBookmarkDecodes(t *testing.T) {
	var b Bookmark
	in := `{"id":3,"url":"https://pkg.go.dev","title":"Docs","created":"2026-10-01T12:00:00Z"}`
	if err := json.Unmarshal([]byte(in), &b); err != nil {
		t.Fatal(err)
	}
	if b.ID != 3 || b.URL != "https://pkg.go.dev" || b.Title != "Docs" || !b.Created.Equal(created) {
		t.Fatalf("decoded %+v from %s", b, in)
	}
}
