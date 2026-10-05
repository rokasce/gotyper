package main

import "net/http"

// create is the POST /bookmarks handler, which the learner writes in step 9.
// Until then this placeholder lets routes register it and answers 501.
func (a *api) create(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented until step 9", http.StatusNotImplemented)
}
