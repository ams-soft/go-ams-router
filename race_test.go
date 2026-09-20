package router

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestConcurrentFirstRequestRace força várias goroutines a chamar
// ServeHTTP simultaneamente na primeiríssima requisição, exatamente o
// cenário em que a construção preguiçosa de mx.handler poderia expor uma
// data race (escrita concorrente no mesmo campo sem sincronização).
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
