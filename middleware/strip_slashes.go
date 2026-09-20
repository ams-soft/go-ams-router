package middleware

import (
	"net/http"
	"net/url"
	"strings"
)

// StripSlashes removes a trailing slash from the path before routing,
// making "/foo" and "/foo/" be treated as the same route. It must be
// registered with Use() on the root Mux — the router, by default,
// strictly distinguishes "/foo" from "/foo/" (both only match if both
// patterns are registered explicitly), and this middleware exists
// precisely for those who prefer the tolerant behavior.
//
// Registering with Use() on an "inline" Router (returned by With/Group)
// has no effect on the routing itself, because this middleware needs to
// run BEFORE the tree decides which route matches — and inline
// middleware only runs AFTER the route has already been found. Always
// use it on the root Mux.
func StripSlashes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := r.URL.Path; len(p) > 1 && p[len(p)-1] == '/' {
			r = cloneWithPath(r, strings.TrimRight(p, "/"))
		}
		next.ServeHTTP(w, r)
	})
}

// RedirectSlashes responds with 301, removing a path's trailing slash,
// instead of routing silently the way StripSlashes does — useful when
// you want a single canonical URL (good for SEO and to avoid duplicate
// content under two different URLs).
func RedirectSlashes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := r.URL.Path; len(p) > 1 && p[len(p)-1] == '/' {
			clean := strings.TrimRight(p, "/")
			if r.URL.RawQuery != "" {
				clean += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, clean, http.StatusMovedPermanently)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// cloneWithPath makes a shallow copy of the request with a different
// path, without mutating the original r.URL (which may be shared by
// other middlewares already executed earlier in the chain).
func cloneWithPath(r *http.Request, path string) *http.Request {
	r2 := new(http.Request)
	*r2 = *r
	u2 := new(url.URL)
	*u2 = *r.URL
	u2.Path = path
	r2.URL = u2
	return r2
}
