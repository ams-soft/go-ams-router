package middleware

import "net/http"

// statusWriter wraps an http.ResponseWriter to capture the status code
// and the number of bytes written, information the standard interface
// doesn't expose. WriteHeader is only considered "explicitly called" on
// the first invocation — subsequent calls (improper handler behavior,
// but one the stdlib tolerates) don't overwrite the already-captured
// status, mirroring http.ResponseWriter's actual behavior.
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

// Unwrap allows http.ResponseController (Go 1.20+) and checks via
// errors.As/http.NewResponseController to see the original
// ResponseWriter beneath the wrapper — needed for handlers that depend
// on features like Flush, Hijack, or SetReadDeadline.
func (sw *statusWriter) Unwrap() http.ResponseWriter {
	return sw.ResponseWriter
}
