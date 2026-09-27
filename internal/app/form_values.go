package app

// Small helpers for reading ids and values from the URL and form.

import (
	"net/http"
	"strconv"
)

func queryDefault(r *http.Request, key, fallback string) string {
	if v := r.URL.Query().Get(key); v != "" {
		return v
	}
	return fallback
}

func pathID(r *http.Request) int64 { return parseID(r.PathValue("id")) }

func parseID(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
