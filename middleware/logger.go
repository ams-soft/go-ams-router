package middleware

import (
	"log"
	"net/http"
	"time"
)

// Logger loga o início e fim de cada requisição, com método, path, status
// resultante e tempo de processamento. Usa o logger padrão de log.Default();
// para logging estruturado (JSON, níveis, etc.), escreva um middleware
// equivalente usando slog — a interface de middleware (func(http.Handler)
// http.Handler) é a mesma independente do backend de log escolhido.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := newStatusWriter(w)

		next.ServeHTTP(sw, r)

		log.Printf("%s %s %d %d bytes in %s",
			r.Method, r.URL.Path, sw.status, sw.bytes, time.Since(start))
	})
}
