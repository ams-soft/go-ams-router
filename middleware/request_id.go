package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type requestIDKey struct{}

// RequestIDHeader é o header usado para propagar o request ID, tanto na
// leitura (se o cliente/proxy já enviou um) quanto na escrita da resposta.
const RequestIDHeader = "X-Request-Id"

// RequestID injeta um identificador único por requisição no contexto e no
// header de resposta. Se o header já vier preenchido na requisição (ex.:
// definido por um proxy/load balancer upstream), esse valor é preservado
// em vez de gerar um novo — permitindo rastrear a requisição de ponta a
// ponta através de múltiplos serviços.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set(RequestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID retorna o request ID associado ao contexto, ou string
// vazia se nenhum tiver sido definido (ex.: middleware RequestID não está
// na stack).
func GetRequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// newRequestID gera um identificador aleatório de 16 bytes, em hex (32
// caracteres). Usa apenas crypto/rand da stdlib — sem dependência de
// pacote de UUID externo.
func newRequestID() string {
	var b [16]byte
	// Erro de crypto/rand.Read é praticamente impossível em condições
	// normais (fonte de entropia do SO indisponível); nesse caso raríssimo,
	// preferimos seguir com zeros a derrubar a requisição.
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
