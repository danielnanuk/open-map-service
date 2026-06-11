package route

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const tripJSON = `{"trip":{"legs":[{"shape":"abc","summary":{"time":620.5,"length":2.41}}],
  "summary":{"time":620.5,"length":2.41},"status":0,"units":"kilometers"}}`

func fakeValhalla(t *testing.T, body string, status int, capture *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/route" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if capture != nil {
			json.NewDecoder(r.Body).Decode(capture)
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
}

func TestRouteParsesTrip(t *testing.T) {
	var got map[string]any
	srv := fakeValhalla(t, tripJSON, 200, &got)
	defer srv.Close()
	c := New(srv.URL)
	trip, err := c.Route(context.Background(),
		[]Location{{Lat: 11.5564, Lon: 104.9282}, {Lat: 11.5696, Lon: 104.9210}},
		"auto", nil, "en-US")
	if err != nil || trip == nil {
		t.Fatalf("err=%v trip=%v", err, trip)
	}
	if trip.Summary.Length != 2.41 || trip.Legs[0].Shape != "abc" {
		t.Fatalf("parse: %+v", trip)
	}
	if got["costing"] != "auto" || got["units"] != "kilometers" || got["language"] != "en-US" {
		t.Fatalf("request body: %v", got)
	}
}

func TestRouteNoPathIsNilNil(t *testing.T) {
	srv := fakeValhalla(t, `{"error_code":442,"error":"No path could be found for input","status_code":400}`, 400, nil)
	defer srv.Close()
	c := New(srv.URL)
	trip, err := c.Route(context.Background(), []Location{{Lat: 1, Lon: 2}, {Lat: 3, Lon: 4}}, "auto", nil, "")
	if err != nil || trip != nil {
		t.Fatalf("want nil,nil got %v,%v", trip, err)
	}
}

func TestRouteServerErrorSurfaces(t *testing.T) {
	srv := fakeValhalla(t, "boom", 500, nil)
	defer srv.Close()
	c := New(srv.URL)
	if _, err := c.Route(context.Background(), []Location{{Lat: 1, Lon: 2}, {Lat: 3, Lon: 4}}, "auto", nil, ""); err == nil {
		t.Fatal("want error")
	}
}
