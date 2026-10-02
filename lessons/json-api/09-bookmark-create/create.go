package main

import (
	"encoding/json"
	"net/http"
	"net/url"
)

func (a *api) create(w http.ResponseWriter, r *http.Request) {
	var b Bookmark
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		http.Error(w, "bad JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	u, err := url.Parse(b.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		http.Error(w, "url must be an absolute http(s) URL", http.StatusBadRequest)
		return
	}
	b = a.store.Add(b)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(b)
}
