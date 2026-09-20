// Command basic demonstra o uso essencial do router: rotas estáticas e
// com parâmetros, middlewares globais e inline, Group e Mount de
// sub-router.
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

	// Middlewares globais: aplicados a toda rota registrada depois desta
	// chamada. Recoverer primeiro, pra capturar panic de qualquer coisa
	// que rodar depois dele na cadeia.
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

	// Group: stack de middleware própria, isolada do restante do router.
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

	// Route + Mount: sub-router independente, registrado a partir de "/".
	r.Route("/orgs/{orgID}", func(sr router.Router) {
		sr.Get("/users/{userID}", func(w http.ResponseWriter, req *http.Request) {
			fmt.Fprintf(w, "org=%s user=%s\n",
				router.URLParam(req, "orgID"), router.URLParam(req, "userID"))
		})
	})

	// Wildcard: captura o restante do path em URLParam(r, "*").
	r.Get("/files/*", func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(w, "file: %s\n", router.URLParam(req, "*"))
	})

	log.Println("listening on :3000")
	log.Fatal(http.ListenAndServe(":3000", r))
}
