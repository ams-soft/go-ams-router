package router

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestConcurrentFirstRequestRace forces multiple goroutines to call
// ServeHTTP simultaneously on the very first request, exactly the
// scenario where the lazy construction of mx.handler could expose a
// data race (concurrent write to the same field without
// synchronization).
func TestConcurrentFirstRequestRace(t *testing.T) {
	r := NewRouter()
	r.Use(func(next http.Handler) http.Handler { return next })
	r.Get("/x", func(w http.ResponseWriter, r *http.Request) {})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
		}()
	}
	wg.Wait()
}
