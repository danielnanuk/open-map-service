package geocode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const searchJSON = `[{"place_id":123,"osm_type":"way","osm_id":456,
  "lat":"11.5563","lon":"104.9282","name":"Royal Palace",
  "display_name":"Royal Palace, Phnom Penh, Cambodia","addresstype":"tourism",
  "address":{"tourism":"Royal Palace","road":"Sothearos Blvd","city":"Phnom Penh",
             "country":"Cambodia","country_code":"kh","postcode":"12301"}}]`

func fakeNominatim(t *testing.T, wantPath string, body string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wantPath {
			t.Errorf("path = %s, want %s", r.URL.Path, wantPath)
		}
		if r.URL.Query().Get("format") != "jsonv2" {
			t.Errorf("format param missing: %s", r.URL.RawQuery)
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
}

func TestSearchParsesResults(t *testing.T) {
	srv := fakeNominatim(t, "/search", searchJSON, 200)
	defer srv.Close()
	c := NewNominatim(srv.URL)
	res, err := c.Search(context.Background(), "royal palace", "en", 5)
	if err != nil || len(res) != 1 {
		t.Fatalf("err=%v res=%v", err, res)
	}
	r := res[0]
	if r.OsmType != "way" || r.OsmID != 456 || r.DisplayName == "" {
		t.Fatalf("parse: %+v", r)
	}
	lat, lon, err := r.Coords()
	if err != nil || lat != 11.5563 || lon != 104.9282 {
		t.Fatalf("coords: %v %v %v", lat, lon, err)
	}
	if r.Address["city"] != "Phnom Penh" {
		t.Fatalf("address: %v", r.Address)
	}
}

func TestReverseNotFoundIsNilNil(t *testing.T) {
	srv := fakeNominatim(t, "/reverse", `{"error":"Unable to geocode"}`, 200)
	defer srv.Close()
	c := NewNominatim(srv.URL)
	r, err := c.Reverse(context.Background(), 0.1, 0.1, "en")
	if err != nil || r != nil {
		t.Fatalf("want nil,nil got %v,%v", r, err)
	}
}

func TestSearchHTTPErrorSurfaces(t *testing.T) {
	srv := fakeNominatim(t, "/search", "boom", 500)
	defer srv.Close()
	c := NewNominatim(srv.URL)
	if _, err := c.Search(context.Background(), "x", "en", 5); err == nil {
		t.Fatal("want error on 500")
	}
}
