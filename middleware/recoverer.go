package middleware

import (
	"log"
	"net/http"
	"runtime/debug"
)

// Recoverer absorve panics ocorridos em handlers ou middlewares
// subsequentes, loga o valor do panic e o stack trace, e responde
// 500 Internal Server Error em vez de derrubar a conexão (e, se não
// houvesse recover em nenhum lugar da cadeia, potencialmente o processo
// inteiro, dependendo de como o servidor HTTP trata panics não capturados
// em outras goroutines).
//
// Deve ser um dos primeiros middlewares na stack (via Use), para que
// consiga capturar panics de qualquer middleware ou handler registrado
// depois dele.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic: %v\n%s", rec, debug.Stack())
				w.WriteHeader(http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
