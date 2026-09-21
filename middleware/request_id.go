package middleware

import (
	"context"
	"net/http"
	"uuid"
)

type requestIDKey struct{}

// RequestIDHeader is the header used to propagate the request ID, both
// when reading (if the client/proxy already sent one) and when writing
// the response.
const RequestIDHeader = "X-Request-Id"

// RequestID injects a unique per-request identifier into the context and
// into the response header. If the header is already set on the request
// (e.g. set by an upstream proxy/load balancer), that value is preserved
// instead of generating a new one — allowing the request to be traced
// end-to-end across multiple services.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set(RequestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID returns the request ID associated with the context, or an
// empty string if none has been set (e.g. the RequestID middleware isn't
// in the stack).
func GetRequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// newRequestID generates a random UUID v7 identifier, in string format. Uses the uuid package for generation.
func newRequestID() string {
	return uuid.NewV7().String()
}
