package main

import (
	"encoding/json"
	"net/http"
	"strconv"
)

type api struct{ store *Store }

func (a *api) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /bookmarks", a.list)
	mux.HandleFunc("GET /bookmarks/{id}", a.get)
	mux.HandleFunc("POST /bookmarks", a.create)
	return mux
}

func (a *api) list(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(a.store.List())
}

func (a *api) get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "id must be a number", http.StatusBadRequest)
		return
	}
	b, ok := a.store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(b)
}
