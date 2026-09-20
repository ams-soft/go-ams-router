package middleware

import (
	"log"
	"net/http"
	"time"
)

// Logger logs the start and end of each request, with method, path,
// resulting status, and processing time. It uses the default logger
// from log.Default(); for structured logging (JSON, levels, etc.), write
// an equivalent middleware using slog — the middleware interface
// (func(http.Handler) http.Handler) is the same regardless of the
// chosen logging backend.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := newStatusWriter(w)

		next.ServeHTTP(sw, r)

		log.Printf("%s %s %d %d bytes in %s",
			r.Method, r.URL.Path, sw.status, sw.bytes, time.Since(start))
	})
}
