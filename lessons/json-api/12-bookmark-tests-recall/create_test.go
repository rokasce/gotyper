package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreate(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{"a good bookmark", `{"url": "https://go.dev", "title": "Go"}`, http.StatusCreated},
		{"malformed JSON", `{"url": `, http.StatusBadRequest},
		{"a url that is not http", `{"url": "ftp://go.dev"}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &api{store: &Store{}}
			req := httptest.NewRequest("POST", "/bookmarks", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			a.create(rec, req)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d; body: %s", rec.Code, tt.want, rec.Body)
			}
		})
	}
}
