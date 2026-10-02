package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGreetHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	greetHandler(rec, httptest.NewRequest("GET", "/greet?name=gopher", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"message":"hello, gopher"}` {
		t.Fatalf("body = %s", got)
	}

	rec = httptest.NewRecorder()
	greetHandler(rec, httptest.NewRequest("GET", "/greet", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing name: status = %d, want 400", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "name is required" {
		t.Fatalf("missing name: body = %q, want only the error (did you return?)", got)
	}
}
