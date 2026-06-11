package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielnanuk/open-map-service/gateway/internal/auth"
)

type fakeChecker struct{ dec auth.Decision }

func (f *fakeChecker) Check(context.Context, string) auth.Decision { return f.dec }

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

func TestAuthDisabledPassesThrough(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	rec := httptest.NewRecorder()
	WithAuth(false, &fakeChecker{auth.DecisionDenied})(inner).
		ServeHTTP(rec, httptest.NewRequest("GET", "/v1/places/x", nil))
	if rec.Code != 204 {
		t.Fatalf("disabled auth must pass: %d", rec.Code)
	}
}

func TestAuthDeniedAndRateLimited(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	rec := httptest.NewRecorder()
	WithAuth(true, &fakeChecker{auth.DecisionDenied})(inner).
		ServeHTTP(rec, httptest.NewRequest("GET", "/v1/places/x", nil))
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), "PERMISSION_DENIED") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	WithAuth(true, &fakeChecker{auth.DecisionRateLimited})(inner).
		ServeHTTP(rec, httptest.NewRequest("GET", "/v1/places/x?key=k", nil))
	if rec.Code != 429 || !strings.Contains(rec.Body.String(), "RESOURCE_EXHAUSTED") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestAuthExemptsHealthAndMetrics(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	for _, p := range []string{"/healthz", "/metrics"} {
		rec := httptest.NewRecorder()
		WithAuth(true, &fakeChecker{auth.DecisionDenied})(inner).
			ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != 204 {
			t.Fatalf("%s must be exempt: %d", p, rec.Code)
		}
	}
}
