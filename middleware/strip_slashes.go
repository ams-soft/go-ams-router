package middleware

import (
	"net/http"
	"net/url"
	"strings"
)

// StripSlashes remove uma barra final do path antes do roteamento,
// fazendo "/foo" e "/foo/" serem tratados como a mesma rota. Deve ser
// registrado com Use() no Mux raiz — o router, por padrão, distingue
// "/foo" de "/foo/" estritamente (só casam ambos se ambos os patterns
// forem registrados explicitamente), e este middleware existe
// justamente para quem prefere o comportamento tolerante.
//
// Registrar com Use() num Router "inline" (retornado por With/Group) não
// tem efeito sobre o roteamento em si, porque esse middleware precisa
// rodar ANTES da árvore decidir qual rota casar — e middleware inline só
// roda DEPOIS que a rota já foi encontrada. Use sempre no Mux raiz.
func StripSlashes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := r.URL.Path; len(p) > 1 && p[len(p)-1] == '/' {
			r = cloneWithPath(r, strings.TrimRight(p, "/"))
		}
		next.ServeHTTP(w, r)
	})
}

// RedirectSlashes responde com 301 removendo a barra final de um path,
// em vez de rotear silenciosamente como StripSlashes faz — útil quando
// se quer uma URL canônica única (bom para SEO e para evitar conteúdo
// duplicado sob duas URLs diferentes).
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

// cloneWithPath faz uma cópia rasa da requisição com um path diferente,
// sem mutar r.URL original (que pode ser compartilhado por outros
// middlewares já executados antes deste na cadeia).
func cloneWithPath(r *http.Request, path string) *http.Request {
	r2 := new(http.Request)
	*r2 = *r
	u2 := new(url.URL)
	*u2 = *r.URL
	u2.Path = path
	r2.URL = u2
	return r2
}
