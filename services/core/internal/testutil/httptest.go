package testutil

import (
	"net/http"
	"net/http/httptest"
)

// StartHTTPServer starts a local HTTP server with the supplied route patterns.
// Patterns use http.ServeMux syntax, so callers can register method-aware routes
// such as "POST /api/jobs" alongside other endpoints.
func StartHTTPServer(routes map[string]http.HandlerFunc) (url string, cleanup func()) {
	mux := http.NewServeMux()
	for pattern, handler := range routes {
		mux.HandleFunc(pattern, handler)
	}

	server := httptest.NewServer(mux)
	return server.URL, server.Close
}
