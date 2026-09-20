package middleware

import (
	"context"
	"net/http"
	"time"
)

// Timeout signals, via ctx.Done(), that the deadline defined by d has
// been reached, so that handlers making context-aware calls (database,
// HTTP client, etc.) can abort work in progress. Unlike
// Recoverer/RequestID/Logger, this middleware uses context.WithTimeout —
// which allocates — because it is an inherently one-off, low-frequency
// operation compared to the routing itself; it isn't part of the
// router core's zero-alloc hot path.
//
// Important: Timeout by itself does not interrupt a handler that
// ignores ctx.Done() and keeps processing — it only communicates that
// the deadline has expired. Handlers need to actively check
// ctx.Err()/ctx.Done(), or use APIs (like database/sql, http.Client)
// that already respect the context internally.
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
