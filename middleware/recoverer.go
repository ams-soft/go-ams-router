package middleware

import (
	"log"
	"net/http"
	"runtime/debug"
)

// Recoverer absorbs panics occurring in subsequent handlers or
// middlewares, logs the panic value and stack trace, and responds with
// 500 Internal Server Error instead of dropping the connection (and, if
// there were no recover anywhere in the chain, potentially the whole
// process, depending on how the HTTP server handles uncaught panics in
// other goroutines).
//
// Should be one of the first middlewares in the stack (via Use), so it
// can catch panics from any middleware or handler registered after it.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic: %v\n%s", rec, debug.Stack())
				w.WriteHeader(http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
