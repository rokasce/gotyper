package main

import (
	"encoding/json"
	"log"
	"os"
	"time"
)

func main() {
	b := Bookmark{ID: 1, URL: "https://go.dev", Created: time.Now()}
	if err := json.NewEncoder(os.Stdout).Encode(b); err != nil {
		log.Fatal(err)
	}
}
