package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDInjected(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestIDFrom(r.Context()) == "" {
			t.Fatal("request id missing in context")
		}
		w.WriteHeader(204)
	})
	rec := httptest.NewRecorder()
	WithRequestID(inner).ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("X-Request-Id header missing")
	}
}

func TestRequestIDPassthrough(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestIDFrom(r.Context()) != "client-supplied" {
			t.Fatalf("got %q", RequestIDFrom(r.Context()))
		}
	})
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("X-Request-Id", "client-supplied")
	WithRequestID(inner).ServeHTTP(httptest.NewRecorder(), req)
}
