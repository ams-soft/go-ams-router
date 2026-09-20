package middleware

import "net/http"

// statusWriter envolve um http.ResponseWriter para capturar o status code
// e o número de bytes escritos, informação que a interface padrão não
// expõe. WriteHeader só é considerado "chamado explicitamente" na
// primeira invocação — chamadas subsequentes (comportamento indevido de
// um handler, mas que a stdlib tolera) não sobrescrevem o status já
// capturado, espelhando o comportamento real do http.ResponseWriter.
type statusWriter struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func newStatusWriter(w http.ResponseWriter) *statusWriter {
	return &statusWriter{ResponseWriter: w, status: http.StatusOK}
}

func (sw *statusWriter) WriteHeader(code int) {
	if !sw.wroteHeader {
		sw.status = code
		sw.wroteHeader = true
	}
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusWriter) Write(b []byte) (int, error) {
	if !sw.wroteHeader {
		sw.WriteHeader(http.StatusOK)
	}
	n, err := sw.ResponseWriter.Write(b)
	sw.bytes += n
	return n, err
}

// Unwrap permite que http.ResponseController (Go 1.20+) e checagens via
// errors.As/http.NewResponseController enxerguem o ResponseWriter
// original por baixo do wrapper — necessário para handlers que dependem
// de recursos como Flush, Hijack ou SetReadDeadline.
func (sw *statusWriter) Unwrap() http.ResponseWriter {
	return sw.ResponseWriter
}
