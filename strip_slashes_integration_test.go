package router_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	router "github.com/ams-soft/go-ams-router"
	"github.com/ams-soft/go-ams-router/middleware"
)

func TestStripSlashesMakesTrailingSlashTolerant(t *testing.T) {
	r := router.NewRouter()
	r.Use(middleware.StripSlashes)
	r.Get("/user/{name}", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("user:" + router.URLParam(req, "name")))
	})

	req := httptest.NewRequest(http.MethodGet, "/user/jsmith/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (StripSlashes deveria ter normalizado antes do roteamento)", rec.Code)
	}
	if got := rec.Body.String(); got != "user:jsmith" {
		t.Fatalf("body = %q, want %q", got, "user:jsmith")
	}
}

func TestRedirectSlashesRedirects(t *testing.T) {
	r := router.NewRouter()
	r.Use(middleware.RedirectSlashes)
	r.Get("/user/{name}", func(w http.ResponseWriter, req *http.Request) {})

	req := httptest.NewRequest(http.MethodGet, "/user/jsmith/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/user/jsmith" {
		t.Fatalf("Location = %q, want %q", loc, "/user/jsmith")
	}
}

func TestInlineMiddlewareCannotStripSlashes(t *testing.T) {
	// Explicitly documents the limitation: StripSlashes registered via
	// With()/Group() (not on the root Mux) doesn't affect routing,
	// because in that case the middleware only runs AFTER the route has
	// already matched.
	r := router.NewRouter()
	r.With(middleware.StripSlashes).Get("/user/{name}", func(w http.ResponseWriter, req *http.Request) {})

	req := httptest.NewRequest(http.MethodGet, "/user/jsmith/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (inline StripSlashes should not affect routing — use it on the root Mux)", rec.Code)
	}
}
