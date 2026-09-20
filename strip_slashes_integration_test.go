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
	// Documenta explicitamente a limitação: StripSlashes registrado via
	// With()/Group() (não no Mux raiz) não afeta o roteamento, porque
	// nesse caso o middleware só roda DEPOIS que a rota já casou.
	r := router.NewRouter()
	r.With(middleware.StripSlashes).Get("/user/{name}", func(w http.ResponseWriter, req *http.Request) {})

	req := httptest.NewRequest(http.MethodGet, "/user/jsmith/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (StripSlashes inline não deveria afetar o roteamento — use no Mux raiz)", rec.Code)
	}
}
