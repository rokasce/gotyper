package main

import (
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("GET /greet", greetHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
