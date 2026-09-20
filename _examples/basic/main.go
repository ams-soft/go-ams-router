// Command basic demonstrates essential router usage: static and
// parameterized routes, global and inline middlewares, Group, and
// sub-router Mount.
package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	router "github.com/ams-soft/go-ams-router"
	"github.com/ams-soft/go-ams-router/middleware"
)

func main() {
	r := router.NewRouter()

	// Global middlewares: applied to every route registered after this
	// call. Recoverer first, to catch a panic from anything that runs
	// after it in the chain.
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Timeout(5 * time.Second))

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintln(w, "welcome")
	})

	r.Get("/users/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := router.URLParam(req, "id")
		fmt.Fprintf(w, "user: %s\n", id)
	})

	// Group: its own middleware stack, isolated from the rest of the router.
	r.Group(func(gr router.Router) {
		gr.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("X-Admin-Area", "true")
				next.ServeHTTP(w, req)
			})
		})
		gr.Get("/admin-status", func(w http.ResponseWriter, req *http.Request) {
			fmt.Fprintln(w, "admin area ok")
		})
	})

	// Route + Mount: independent sub-router, registered starting from "/".
	r.Route("/orgs/{orgID}", func(sr router.Router) {
		sr.Get("/users/{userID}", func(w http.ResponseWriter, req *http.Request) {
			fmt.Fprintf(w, "org=%s user=%s\n",
				router.URLParam(req, "orgID"), router.URLParam(req, "userID"))
		})
	})

	// Wildcard: captures the rest of the path in URLParam(r, "*").
	r.Get("/files/*", func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(w, "file: %s\n", router.URLParam(req, "*"))
	})

	log.Println("listening on :3000")
	log.Fatal(http.ListenAndServe(":3000", r))
}
