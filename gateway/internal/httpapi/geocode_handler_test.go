package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/danielnanuk/open-map-service/gateway/internal/geocode"
	"github.com/danielnanuk/open-map-service/gateway/internal/search"
)

type fakeGeocoder struct {
	searchRes  []geocode.NominatimResult
	reverseRes *geocode.NominatimResult
	err        error
	searchHits int
}

func (f *fakeGeocoder) Search(_ context.Context, q, lang string, limit int) ([]geocode.NominatimResult, error) {
	f.searchHits++
	return f.searchRes, f.err
}
func (f *fakeGeocoder) Reverse(_ context.Context, lat, lon float64, lang string) (*geocode.NominatimResult, error) {
	return f.reverseRes, f.err
}

func nmResult() geocode.NominatimResult {
	return geocode.NominatimResult{OsmType: "way", OsmID: 9, Lat: "11.5", Lon: "104.9",
		DisplayName: "Street 271, Phnom Penh, Cambodia",
		Address:     map[string]string{"road": "Street 271", "city": "Phnom Penh", "country": "Cambodia", "country_code": "kh"}}
}

func geocodeGET(t *testing.T, h *Handlers, query string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("GET", "/maps/api/geocode/json"+query, nil)
	rec := httptest.NewRecorder()
	h.Geocode(rec, req)
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestGeocodeAddressPrefersNominatim(t *testing.T) {
	g := &fakeGeocoder{searchRes: []geocode.NominatimResult{nmResult()}}
	h := NewWithGeocoder(&fakeSearcher{}, nil, g)
	code, body := geocodeGET(t, h, "?address=Street+271+Phnom+Penh")
	if code != 200 || body["status"] != "OK" {
		t.Fatalf("%d %v", code, body)
	}
	r0 := body["results"].([]any)[0].(map[string]any)
	if r0["place_id"] != "nominatim:way:9" {
		t.Fatalf("place_id: %v", r0)
	}
	loc := r0["geometry"].(map[string]any)["location"].(map[string]any)
	if loc["lat"] != 11.5 || loc["lng"] != 104.9 { // legacy 键名!
		t.Fatalf("location keys: %v", loc)
	}
}

func TestGeocodePOIPrefersOpenSearchAndSkipsNominatim(t *testing.T) {
	g := &fakeGeocoder{searchRes: []geocode.NominatimResult{nmResult()}}
	h := NewWithGeocoder(&fakeSearcher{docs: []search.Doc{doc()}}, nil, g)
	_, body := geocodeGET(t, h, "?address=Royal+Palace")
	r0 := body["results"].([]any)[0].(map[string]any)
	if r0["place_id"] != "p1" { // OS 结果在前
		t.Fatalf("want OS-first, got %v", r0)
	}
	if g.searchHits != 0 { // OS 有结果时不应触发 Nominatim
		t.Fatalf("nominatim should not be called, hits=%d", g.searchHits)
	}
}

func TestGeocodeFallbackOnZeroResults(t *testing.T) {
	g := &fakeGeocoder{searchRes: []geocode.NominatimResult{nmResult()}}
	h := NewWithGeocoder(&fakeSearcher{}, nil, g) // OS 零结果
	_, body := geocodeGET(t, h, "?address=Some+POI+Name")
	if body["status"] != "OK" { // 回退到 Nominatim
		t.Fatalf("fallback failed: %v", body)
	}
}

func TestGeocodeBothEmpty(t *testing.T) {
	h := NewWithGeocoder(&fakeSearcher{}, nil, &fakeGeocoder{})
	_, body := geocodeGET(t, h, "?address=zzzz+nothing")
	if body["status"] != "ZERO_RESULTS" {
		t.Fatalf("%v", body)
	}
}

func TestGeocodeBackendsDownIsUnknownError(t *testing.T) {
	g := &fakeGeocoder{err: errors.New("down")}
	h := NewWithGeocoder(&errSearcher{}, nil, g)
	code, body := geocodeGET(t, h, "?address=Street+271")
	if code != 200 || body["status"] != "UNKNOWN_ERROR" { // legacy:HTTP 200 + status
		t.Fatalf("%d %v", code, body)
	}
}

func TestGeocodeMissingParams(t *testing.T) {
	h := NewWithGeocoder(&fakeSearcher{}, nil, &fakeGeocoder{})
	code, body := geocodeGET(t, h, "")
	if code != 200 || body["status"] != "INVALID_REQUEST" {
		t.Fatalf("%d %v", code, body)
	}
}
