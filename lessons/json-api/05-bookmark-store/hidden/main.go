package main

import (
	"encoding/json"
	"log"
	"os"
	"time"
)

func main() {
	var s Store
	s.Add(Bookmark{URL: "https://go.dev", Title: "Go", Created: time.Now()})
	s.Add(Bookmark{URL: "https://pkg.go.dev", Created: time.Now()})
	if err := json.NewEncoder(os.Stdout).Encode(s.List()); err != nil {
		log.Fatal(err)
	}
}
