package main

import (
	"sync"
	"testing"
)

func TestStoreAddGetList(t *testing.T) {
	var s Store
	a := s.Add(Bookmark{ID: 99, URL: "https://go.dev", Title: "Go"})
	b := s.Add(Bookmark{URL: "https://pkg.go.dev"})
	if a.ID != 1 || b.ID != 2 {
		t.Fatalf("Add gave ids %d and %d, want 1 and 2 (the store picks the id)", a.ID, b.ID)
	}
	if a.URL != "https://go.dev" || a.Title != "Go" {
		t.Fatalf("Add returned %+v, want the bookmark as stored", a)
	}

	got, ok := s.Get(2)
	if !ok || got != b {
		t.Fatalf("Get(2) = %+v, %v; want %+v, true", got, ok, b)
	}
	for _, id := range []int{0, 3, -1} {
		if got, ok := s.Get(id); ok || got != (Bookmark{}) {
			t.Fatalf("Get(%d) = %+v, %v; want the zero Bookmark, false", id, got, ok)
		}
	}

	list := s.List()
	if len(list) != 2 || list[0] != a || list[1] != b {
		t.Fatalf("List() = %+v, want [%+v %+v] in id order", list, a, b)
	}
	list[0].Title = "changed"
	if got, _ := s.Get(1); got.Title != "Go" {
		t.Fatalf("changing List's result changed the store: Get(1).Title = %q", got.Title)
	}
}

// TestStoreConcurrentAdd adds from many goroutines at once, as HTTP handlers
// will. Without the mutex, two Adds can read the same length and lose a
// bookmark or share an id.
func TestStoreConcurrentAdd(t *testing.T) {
	const goroutines, each = 50, 100
	var s Store
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			for range each {
				s.Add(Bookmark{URL: "https://go.dev"})
			}
		})
	}
	wg.Wait()

	list := s.List()
	if len(list) != goroutines*each {
		t.Fatalf("List() has %d bookmarks after %d Adds", len(list), goroutines*each)
	}
	for i, b := range list {
		if b.ID != i+1 {
			t.Fatalf("List()[%d].ID = %d, want %d: ids must be distinct and in order", i, b.ID, i+1)
		}
	}
}
