package main

import (
	"log"
	"net/http"
)

func main() {
	a := &api{store: &Store{}}
	log.Fatal(http.ListenAndServe(":8080", a.routes()))
}
