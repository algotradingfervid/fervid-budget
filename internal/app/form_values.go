package app

// Small helpers for reading ids and values from the URL and form.

import (
	"net/http"
	"strconv"
)

// queryDefault returns the query parameter key, or fallback when it is empty.
func queryDefault(r *http.Request, key, fallback string) string {
	if v := r.URL.Query().Get(key); v != "" {
		return v
	}
	return fallback
}

// pathID is the {id} in the URL path as a number (0 when it is not one).
func pathID(r *http.Request) int64 { return parseID(r.PathValue("id")) }

// parseID reads an id; anything that is not a whole number gives 0, which
// no record has.
func parseID(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
