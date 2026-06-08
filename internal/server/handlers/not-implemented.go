package handlers

import "net/http"

// NotImplemented is a temporary placeholder for the /convert route.
func NotImplemented(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
