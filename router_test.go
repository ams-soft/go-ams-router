package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func doReq(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestStaticRoute(t *testing.T) {
	r := NewRouter()
	r.Get("/hello", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hi"))
	})

	rec := doReq(t, r, http.MethodGet, "/hello")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "hi" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "hi")
	}
}

func TestParamRoute(t *testing.T) {
	r := NewRouter()
	r.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("user:" + URLParam(r, "id")))
	})

	rec := doReq(t, r, http.MethodGet, "/users/42")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "user:42" {
		t.Fatalf("body = %q, want %q", got, "user:42")
	}
}

func TestMultipleParams(t *testing.T) {
	r := NewRouter()
	r.Get("/orgs/{orgID}/users/{userID}", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(URLParam(r, "orgID") + "/" + URLParam(r, "userID")))
	})

	rec := doReq(t, r, http.MethodGet, "/orgs/acme/users/7")
	if got := rec.Body.String(); got != "acme/7" {
		t.Fatalf("body = %q, want %q", got, "acme/7")
	}
}

func TestStaticTakesPrecedenceOverParam(t *testing.T) {
	r := NewRouter()
	r.Get("/users/search", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("search"))
	})
	r.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("id:" + URLParam(r, "id")))
	})

	rec := doReq(t, r, http.MethodGet, "/users/search")
	if got := rec.Body.String(); got != "search" {
		t.Fatalf("body = %q, want %q (static deveria vencer o param)", got, "search")
	}

	rec2 := doReq(t, r, http.MethodGet, "/users/42")
	if got := rec2.Body.String(); got != "id:42" {
		t.Fatalf("body = %q, want %q", got, "id:42")
	}
}

func TestCatchAll(t *testing.T) {
	r := NewRouter()
	r.Get("/files/*", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("file:" + URLParam(r, "*")))
	})

	rec := doReq(t, r, http.MethodGet, "/files/a/b/c.txt")
	if got := rec.Body.String(); got != "file:a/b/c.txt" {
		t.Fatalf("body = %q, want %q", got, "file:a/b/c.txt")
	}
}

func TestNotFound(t *testing.T) {
	r := NewRouter()
	r.Get("/hello", func(w http.ResponseWriter, r *http.Request) {})

	rec := doReq(t, r, http.MethodGet, "/nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestCustomNotFound(t *testing.T) {
	r := NewRouter()
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	rec := doReq(t, r, http.MethodGet, "/nope")
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", rec.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	r := NewRouter()
	r.Get("/hello", func(w http.ResponseWriter, r *http.Request) {})

	rec := doReq(t, r, http.MethodPost, "/hello")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != "GET" {
		t.Fatalf("Allow = %q, want %q", allow, "GET")
	}
}

func TestUseMiddlewareOrder(t *testing.T) {
	r := NewRouter()
	var order []string

	mw := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name+":before")
				next.ServeHTTP(w, r)
				order = append(order, name+":after")
			})
		}
	}

	r.Use(mw("A"), mw("B"))
	r.Get("/x", func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
	})

	doReq(t, r, http.MethodGet, "/x")

	want := []string{"A:before", "B:before", "handler", "B:after", "A:after"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestUseOnRootAllowedAfterRoutesBeforeServing(t *testing.T) {
	// On the root Mux, Use()'s middleware wraps the entire routing
	// process, applied once on the first ServeHTTP — it isn't compiled
	// per route at registration time. So, unlike an inline Mux
	// (With/Group), calling Use() after routes have already been
	// registered is allowed, as long as no request has been served yet.
	r := NewRouter()
	r.Get("/x", func(w http.ResponseWriter, r *http.Request) {})

	var ran bool
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ran = true
			next.ServeHTTP(w, r)
		})
	})

	doReq(t, r, http.MethodGet, "/x")
	if !ran {
		t.Fatal("middleware registrado via Use() depois da rota, mas antes de servir, deveria ter rodado")
	}
}

func TestUseOnRootPanicsAfterServing(t *testing.T) {
	r := NewRouter()
	r.Get("/x", func(w http.ResponseWriter, r *http.Request) {})
	doReq(t, r, http.MethodGet, "/x") // first request: freezes mx.handler

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when calling Use() on the root Mux after it has already served a request")
		}
	}()
	r.Use(func(next http.Handler) http.Handler { return next })
}

func TestUseOnInlineAfterRoutePanics(t *testing.T) {
	r := NewRouter()
	inline := r.With()
	inline.Get("/x", func(w http.ResponseWriter, r *http.Request) {})

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when calling Use() on an inline Router after registering a route on it")
		}
	}()
	inline.Use(func(next http.Handler) http.Handler { return next })
}

func TestWithInlineMiddleware(t *testing.T) {
	r := NewRouter()
	var order []string

	global := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "global")
			next.ServeHTTP(w, r)
		})
	}
	inline := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "inline")
			next.ServeHTTP(w, r)
		})
	}

	r.Use(global)
	r.With(inline).Get("/special", func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler-special")
	})
	r.Get("/plain", func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler-plain")
	})

	order = nil
	doReq(t, r, http.MethodGet, "/special")
	wantSpecial := []string{"global", "inline", "handler-special"}
	if len(order) != len(wantSpecial) {
		t.Fatalf("/special order = %v, want %v", order, wantSpecial)
	}
	for i := range wantSpecial {
		if order[i] != wantSpecial[i] {
			t.Fatalf("/special order = %v, want %v", order, wantSpecial)
		}
	}

	order = nil
	doReq(t, r, http.MethodGet, "/plain")
	wantPlain := []string{"global", "handler-plain"}
	if len(order) != len(wantPlain) {
		t.Fatalf("/plain order = %v, want %v (global NÃO deve rodar duas vezes nem inline deve vazar)", order, wantPlain)
	}
	for i := range wantPlain {
		if order[i] != wantPlain[i] {
			t.Fatalf("/plain order = %v, want %v", order, wantPlain)
		}
	}
}

func TestMount(t *testing.T) {
	admin := NewRouter()
	admin.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("admin-root"))
	})
	admin.Get("/accounts", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("admin-accounts"))
	})

	root := NewRouter()
	root.Mount("/admin", admin)

	rec := doReq(t, root, http.MethodGet, "/admin")
	if got := rec.Body.String(); got != "admin-root" {
		t.Fatalf("GET /admin body = %q, want %q", got, "admin-root")
	}

	rec2 := doReq(t, root, http.MethodGet, "/admin/accounts")
	if got := rec2.Body.String(); got != "admin-accounts" {
		t.Fatalf("GET /admin/accounts body = %q, want %q", got, "admin-accounts")
	}
}

func TestRouteAndMountShareParams(t *testing.T) {
	root := NewRouter()
	root.Route("/orgs/{orgID}", func(r Router) {
		r.Get("/users/{userID}", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(URLParam(r, "orgID") + "/" + URLParam(r, "userID")))
		})
	})

	rec := doReq(t, root, http.MethodGet, "/orgs/acme/users/7")
	if got := rec.Body.String(); got != "acme/7" {
		t.Fatalf("body = %q, want %q", got, "acme/7")
	}
}

func TestGroupIsolatesMiddleware(t *testing.T) {
	r := NewRouter()
	var ran bool

	r.Group(func(gr Router) {
		gr.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ran = true
				next.ServeHTTP(w, r)
			})
		})
		gr.Get("/grouped", func(w http.ResponseWriter, r *http.Request) {})
	})
	r.Get("/ungrouped", func(w http.ResponseWriter, r *http.Request) {})

	doReq(t, r, http.MethodGet, "/ungrouped")
	if ran {
		t.Fatal("middleware do Group vazou para rota fora do grupo")
	}

	ran = false
	doReq(t, r, http.MethodGet, "/grouped")
	if !ran {
		t.Fatal("Group middleware did not run for a route inside the group")
	}
}

// --- Limitation 1: compound segments (per-segment regex) ---

func TestCompoundSegment(t *testing.T) {
	r := NewRouter()
	r.Get("/articles/{month}-{day}-{year}", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(URLParam(r, "year") + "/" + URLParam(r, "month") + "/" + URLParam(r, "day")))
	})

	rec := doReq(t, r, http.MethodGet, "/articles/01-16-2017")
	if got := rec.Body.String(); got != "2017/01/16" {
		t.Fatalf("body = %q, want %q", got, "2017/01/16")
	}
}

func TestCompoundSegmentNoMatch(t *testing.T) {
	r := NewRouter()
	r.Get("/articles/{month}-{day}-{year}", func(w http.ResponseWriter, r *http.Request) {})

	// With no hyphen at all, there's no way to match the pattern's three parts.
	rec := doReq(t, r, http.MethodGet, "/articles/nodate")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (a segment without separators should not match the compound pattern)", rec.Code)
	}
}

func TestCompoundCoexistsWithFullSegmentParam(t *testing.T) {
	// A standalone {id} in the segment still uses the fast path (ntParam,
	// no regex) even in a tree that also has compound nodes on other
	// branches.
	r := NewRouter()
	r.Get("/articles/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("id:" + URLParam(r, "id")))
	})
	r.Get("/articles/{month}-{day}-{year}", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("date"))
	})

	rec := doReq(t, r, http.MethodGet, "/articles/42")
	if got := rec.Body.String(); got != "id:42" {
		t.Fatalf("body = %q, want %q", got, "id:42")
	}

	rec2 := doReq(t, r, http.MethodGet, "/articles/01-16-2017")
	if got := rec2.Body.String(); got != "date" {
		t.Fatalf("body = %q, want %q", got, "date")
	}
}

// --- Limitation 2: strict trailing slash by default ---

func TestTrailingSlashIsStrictByDefault(t *testing.T) {
	r := NewRouter()
	r.Get("/user/{name}", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("no-slash"))
	})

	rec := doReq(t, r, http.MethodGet, "/user/jsmith")
	if rec.Code != http.StatusOK || rec.Body.String() != "no-slash" {
		t.Fatalf("GET /user/jsmith: status=%d body=%q", rec.Code, rec.Body.String())
	}

	rec2 := doReq(t, r, http.MethodGet, "/user/jsmith/")
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("GET /user/jsmith/: status=%d, want 404 (trailing slash not explicitly registered)", rec2.Code)
	}
}

func TestTrailingSlashRegisteredExplicitly(t *testing.T) {
	r := NewRouter()
	r.Get("/user/{name}", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("no-slash"))
	})
	r.Get("/user/{name}/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("with-slash:" + URLParam(r, "name")))
	})

	rec := doReq(t, r, http.MethodGet, "/user/jsmith/")
	if got := rec.Body.String(); got != "with-slash:jsmith" {
		t.Fatalf("body = %q, want %q", got, "with-slash:jsmith")
	}
}

func TestCatchAllMatchesWithOrWithoutTrailingSlash(t *testing.T) {
	r := NewRouter()
	r.Get("/files/*", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("file:" + URLParam(r, "*")))
	})

	rec := doReq(t, r, http.MethodGet, "/files/a/b")
	if got := rec.Body.String(); got != "file:a/b" {
		t.Fatalf("body = %q, want %q", got, "file:a/b")
	}

	// catch-all is the documented exception: it also matches with a trailing slash.
	rec2 := doReq(t, r, http.MethodGet, "/files/a/b/")
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET /files/a/b/: status=%d, want 200 (catch-all deveria casar com barra final)", rec2.Code)
	}
}

// --- Limitation 4: Routes() reflecting Mount as SubRoutes ---

func TestRoutesReflectsMountAsSubRoutes(t *testing.T) {
	admin := NewRouter()
	admin.Get("/accounts", func(w http.ResponseWriter, r *http.Request) {})

	root := NewRouter()
	root.Get("/health", func(w http.ResponseWriter, r *http.Request) {})
	root.Mount("/admin", admin)

	var mountRoute *Route
	for i := range root.Routes() {
		if root.Routes()[i].Pattern == "/admin" {
			mountRoute = &root.Routes()[i]
		}
	}
	if mountRoute == nil {
		t.Fatal("expected a Route with Pattern \"/admin\" in the Routes() output")
	}
	if mountRoute.SubRoutes == nil {
		t.Fatal("expected SubRoutes to be populated for the /admin mount")
	}
	subRoutes := mountRoute.SubRoutes.Routes()
	if len(subRoutes) != 1 || subRoutes[0].Pattern != "/accounts" {
		t.Fatalf("SubRoutes = %+v, want uma rota /accounts", subRoutes)
	}
}
