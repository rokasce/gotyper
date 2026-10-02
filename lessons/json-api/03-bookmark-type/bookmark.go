package main

import "time"

type Bookmark struct {
	ID      int       `json:"id"`
	URL     string    `json:"url"`
	Title   string    `json:"title,omitempty"`
	Created time.Time `json:"created"`
}
