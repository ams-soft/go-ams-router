package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func noopHandler(w http.ResponseWriter, r *http.Request) {}

func BenchmarkStaticRoute(b *testing.B) {
	r := NewRouter()
	r.Get("/users", noopHandler)

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	w := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkParamRoute(b *testing.B) {
	r := NewRouter()
	r.Get("/users/{id}", noopHandler)

	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	w := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkFiveParams(b *testing.B) {
	r := NewRouter()
	r.Get("/a/{p1}/b/{p2}/c/{p3}/d/{p4}/e/{p5}", noopHandler)

	req := httptest.NewRequest(http.MethodGet, "/a/1/b/2/c/3/d/4/e/5", nil)
	w := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkMiddlewareChain(b *testing.B) {
	r := NewRouter()
	noop := func(next http.Handler) http.Handler { return next }
	r.Use(noop, noop, noop, noop, noop)
	r.Get("/users/{id}", noopHandler)

	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	w := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkMount(b *testing.B) {
	admin := NewRouter()
	admin.Get("/accounts/{id}", noopHandler)

	root := NewRouter()
	root.Mount("/admin", admin)

	req := httptest.NewRequest(http.MethodGet, "/admin/accounts/7", nil)
	w := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		root.ServeHTTP(w, req)
	}
}

// BenchmarkCompoundSegment documents the cost of the one matching path
// that isn't guaranteed zero-alloc: segments with {param} mixed with
// literal text use regexp.FindStringSubmatch, which allocates the
// submatches slice. Routes with a standalone {param} in the segment
// (the common case) don't go through here — see BenchmarkParamRoute.
func BenchmarkCompoundSegment(b *testing.B) {
	r := NewRouter()
	r.Get("/articles/{month}-{day}-{year}", noopHandler)

	req := httptest.NewRequest(http.MethodGet, "/articles/01-16-2017", nil)
	w := httptest.NewRecorder()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}
