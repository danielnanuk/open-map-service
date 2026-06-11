package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielnanuk/open-map-service/gateway/internal/geocode"
	"github.com/danielnanuk/open-map-service/gateway/internal/search"
	"github.com/danielnanuk/open-map-service/gateway/internal/store"
)

type fakeGeocoder struct {
	searchRes  []geocode.NominatimResult
	reverseRes *geocode.NominatimResult
	err        error
	searchHits int
	gotLang    string
}

func (f *fakeGeocoder) Search(_ context.Context, q, lang string, limit int) ([]geocode.NominatimResult, error) {
	f.searchHits++
	f.gotLang = lang
	return f.searchRes, f.err
}
func (f *fakeGeocoder) Reverse(_ context.Context, lat, lon float64, lang string) (*geocode.NominatimResult, error) {
	f.gotLang = lang
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
	code, body := geocodeGET(t, h, "?address=Street+271+Phnom+Penh&language=km")
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
	if g.gotLang != "km" {
		t.Fatalf("language not propagated to nominatim: %q", g.gotLang)
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

type fakeNearbyStore struct {
	fakeStore
	nearby []store.PlaceRow
}

func (f *fakeNearbyStore) GetNearbyPlaces(_ context.Context, lat, lon, radiusM float64, limit int) ([]store.PlaceRow, error) {
	return f.nearby, nil
}

func TestReverseGeocodeCombinesAddressAndPOIs(t *testing.T) {
	nm := nmResult()
	g := &fakeGeocoder{reverseRes: &nm}
	st := &fakeNearbyStore{nearby: []store.PlaceRow{{
		PlaceID: "p9", Names: map[string]string{"default": "Brown Coffee"},
		Categories: []string{"cafe"}, Address: map[string]string{"locality": "Phnom Penh"},
		Lon: 104.916, Lat: 11.5621,
	}}}
	h := NewWithGeocoder(&fakeSearcher{}, st, g)
	_, body := geocodeGET(t, h, "?latlng=11.5621,104.9160&language=km")
	if body["status"] != "OK" {
		t.Fatalf("%v", body)
	}
	results := body["results"].([]any)
	if len(results) != 2 { // 地址结果 + 1 个 POI
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if results[0].(map[string]any)["place_id"] != "nominatim:way:9" {
		t.Fatalf("address first: %v", results[0])
	}
	if results[1].(map[string]any)["place_id"] != "p9" {
		t.Fatalf("poi second: %v", results[1])
	}
	if g.gotLang != "km" {
		t.Fatalf("language not propagated to reverse: %q", g.gotLang)
	}
}

func TestReverseGeocodeBadLatlng(t *testing.T) {
	h := NewWithGeocoder(&fakeSearcher{}, &fakeNearbyStore{}, &fakeGeocoder{})
	for _, bad := range []string{"garbage", "1,2,3", "91.0,104.9", "11.5,191.0", "NaN,104.9", "11.5,NaN"} {
		_, body := geocodeGET(t, h, "?latlng="+bad)
		if body["status"] != "INVALID_REQUEST" {
			t.Fatalf("latlng=%q: %v", bad, body)
		}
	}
}

func TestReverseGeocodeOceanIsZeroResults(t *testing.T) {
	h := NewWithGeocoder(&fakeSearcher{}, &fakeNearbyStore{}, &fakeGeocoder{}) // reverseRes nil, nearby 空
	_, body := geocodeGET(t, h, "?latlng=10.0,103.0")
	if body["status"] != "ZERO_RESULTS" {
		t.Fatalf("%v", body)
	}
}

func TestReverseGeocodeLocalizesPOIName(t *testing.T) {
	nm := nmResult()
	g := &fakeGeocoder{reverseRes: &nm}
	st := &fakeNearbyStore{nearby: []store.PlaceRow{{
		PlaceID: "p9", Names: map[string]string{"default": "Brown Coffee", "km": "កាហ្វេ"},
		Categories: []string{"cafe"}, Address: map[string]string{"locality": "Phnom Penh"},
		Lon: 104.916, Lat: 11.5621,
	}}}
	h := NewWithGeocoder(&fakeSearcher{}, st, g)
	_, body := geocodeGET(t, h, "?latlng=11.5621,104.9160&language=km")
	poi := body["results"].([]any)[1].(map[string]any)
	if !strings.Contains(poi["formatted_address"].(string), "កាហ្វេ") {
		t.Fatalf("want km POI name: %v", poi)
	}
}
