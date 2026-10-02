package main

import "sync"

type Store struct {
	mu        sync.Mutex
	bookmarks []Bookmark
}

func (s *Store) Add(b Bookmark) Bookmark {
	s.mu.Lock()
	defer s.mu.Unlock()
	b.ID = len(s.bookmarks) + 1
	s.bookmarks = append(s.bookmarks, b)
	return b
}

func (s *Store) Get(id int) (Bookmark, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id < 1 || id > len(s.bookmarks) {
		return Bookmark{}, false
	}
	return s.bookmarks[id-1], true
}

func (s *Store) List() []Bookmark {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Bookmark(nil), s.bookmarks...)
}
