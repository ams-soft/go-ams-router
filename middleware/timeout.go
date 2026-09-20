package middleware

import (
	"context"
	"net/http"
	"time"
)

// Timeout sinaliza, através de ctx.Done(), que o prazo definido por d foi
// atingido, para que handlers que fazem chamadas que respeitam context
// (banco de dados, HTTP client, etc.) possam abortar o trabalho em
// andamento. Diferente de Recoverer/RequestID/Logger, este middleware usa
// context.WithTimeout — que aloca — porque é uma operação inerentemente
// pontual e de baixa frequência comparada ao roteamento em si; não faz
// parte do hot path de zero-alloc do core do router.
//
// Importante: Timeout por si só não interrompe um handler que ignora
// ctx.Done() e continua processando — ele só communica a expiração do
// prazo. Handlers precisam checar ctx.Err()/ctx.Done() ativamente, ou usar
// APIs (como database/sql, http.Client) que já respeitam o context
// internamente.
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
