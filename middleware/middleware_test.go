package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequestIDGeneratesWhenAbsent(t *testing.T) {
	var gotID string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = GetRequestID(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if gotID == "" {
		t.Fatal("esperava um request ID gerado, veio vazio")
	}
	if rec.Header().Get(RequestIDHeader) != gotID {
		t.Fatalf("header %s = %q, want %q", RequestIDHeader, rec.Header().Get(RequestIDHeader), gotID)
	}
}

func TestRequestIDPreservesUpstream(t *testing.T) {
	const upstream = "upstream-id-123"
	var gotID string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = GetRequestID(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, upstream)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if gotID != upstream {
		t.Fatalf("gotID = %q, want %q (deveria preservar o ID do upstream)", gotID, upstream)
	}
}

func TestRecovererCatchesPanic(t *testing.T) {
	h := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	// Não deve propagar o panic para o teste.
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestTimeoutSignalsDone(t *testing.T) {
	var ctxErrAfterWait error
	h := Timeout(10 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		ctxErrAfterWait = r.Context().Err()
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if ctxErrAfterWait == nil {
		t.Fatal("esperava ctx.Err() != nil após o timeout")
	}
}

func TestLoggerCapturesStatus(t *testing.T) {
	h := Logger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "ok")
	}
}
