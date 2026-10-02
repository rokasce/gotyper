package main

import (
	"encoding/json"
	"errors"
	"net/http"
)

type greeting struct {
	Message string `json:"message"`
}

func greet(name string) (greeting, error) {
	if name == "" {
		return greeting{}, errors.New("name is required")
	}
	return greeting{Message: "hello, " + name}, nil
}

func greetHandler(w http.ResponseWriter, r *http.Request) {
	g, err := greet(r.URL.Query().Get("name"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(g)
}
