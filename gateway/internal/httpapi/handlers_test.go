package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielnanuk/open-map-service/gateway/internal/search"
	"github.com/danielnanuk/open-map-service/gateway/internal/store"
)

type fakeSearcher struct{ docs []search.Doc }

func (f *fakeSearcher) SearchText(context.Context, string, *search.Geo, int) ([]search.Doc, error) {
	return f.docs, nil
}
func (f *fakeSearcher) SearchNearby(context.Context, search.Geo, float64, []string, int, bool) ([]search.Doc, error) {
	return f.docs, nil
}
func (f *fakeSearcher) Autocomplete(context.Context, string, *search.Geo, int) ([]search.Doc, error) {
	return f.docs, nil
}

func doc() search.Doc {
	d := search.Doc{PlaceID: "p1", NameDefault: "អង្គរវត្ត", NameEn: "Angkor Wat",
		NameZh: "吴哥窟", FormattedAddress: "Siem Reap, Cambodia",
		Categories: []string{"tourist_attraction"}, Confidence: 0.95}
	d.Location.Lat, d.Location.Lon = 13.4125, 103.867
	return d
}

func TestSearchTextResponseShapeAndLanguage(t *testing.T) {
	h := New(&fakeSearcher{docs: []search.Doc{doc()}}, nil)
	req := httptest.NewRequest("POST", "/v1/places:searchText",
		strings.NewReader(`{"textQuery":"angkor","languageCode":"zh"}`))
	rec := httptest.NewRecorder()
	h.SearchText(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	place := resp["places"].([]any)[0].(map[string]any)
	if place["id"] != "p1" || place["name"] != "places/p1" {
		t.Fatalf("place identity wrong: %v", place)
	}
	if place["displayName"].(map[string]any)["text"] != "吴哥窟" { // languageCode=zh 选中文名
		t.Fatalf("displayName should honor languageCode: %v", place)
	}
	loc := place["location"].(map[string]any)
	if loc["latitude"].(float64) != 13.4125 {
		t.Fatalf("location: %v", loc)
	}
	if place["attributions"].([]any)[0].(map[string]any)["provider"] != "OpenStreetMap contributors" {
		t.Fatalf("attributions missing: %v", place)
	}
}

func TestSearchTextEmptyQueryIsInvalidArgument(t *testing.T) {
	h := New(&fakeSearcher{}, nil)
	req := httptest.NewRequest("POST", "/v1/places:searchText", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.SearchText(rec, req)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "INVALID_ARGUMENT") {
		t.Fatalf("want google-style 400, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestSearchNearbyRequiresRestriction(t *testing.T) {
	h := New(&fakeSearcher{}, nil)
	req := httptest.NewRequest("POST", "/v1/places:searchNearby", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.SearchNearby(rec, req)
	if rec.Code != 400 {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestFieldMaskApplied(t *testing.T) {
	h := New(&fakeSearcher{docs: []search.Doc{doc()}}, nil)
	req := httptest.NewRequest("POST", "/v1/places:searchText", strings.NewReader(`{"textQuery":"x"}`))
	req.Header.Set("X-Goog-FieldMask", "places.id")
	rec := httptest.NewRecorder()
	h.SearchText(rec, req)
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	place := resp["places"].([]any)[0].(map[string]any)
	if len(place) != 1 || place["id"] != "p1" {
		t.Fatalf("fieldmask not applied: %v", place)
	}
}

type errSearcher struct{}

func (e *errSearcher) SearchText(context.Context, string, *search.Geo, int) ([]search.Doc, error) {
	return nil, fmt.Errorf("Post \"http://opensearch:9200/...\": dial tcp")
}
func (e *errSearcher) SearchNearby(context.Context, search.Geo, float64, []string, int, bool) ([]search.Doc, error) {
	return nil, fmt.Errorf("Post \"http://opensearch:9200/...\": dial tcp")
}
func (e *errSearcher) Autocomplete(context.Context, string, *search.Geo, int) ([]search.Doc, error) {
	return nil, fmt.Errorf("Post \"http://opensearch:9200/...\": dial tcp")
}

func TestChooseNameBCP47(t *testing.T) {
	h := New(&fakeSearcher{docs: []search.Doc{doc()}}, nil)
	req := httptest.NewRequest("POST", "/v1/places:searchText",
		strings.NewReader(`{"textQuery":"angkor","languageCode":"zh-CN"}`))
	rec := httptest.NewRecorder()
	h.SearchText(rec, req)
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	place := resp["places"].([]any)[0].(map[string]any)
	if place["displayName"].(map[string]any)["text"] != "吴哥窟" {
		t.Fatalf("zh-CN should resolve zh name: %v", place)
	}
}

func TestInternalErrorIsGeneric(t *testing.T) {
	h := New(&errSearcher{}, nil)
	req := httptest.NewRequest("POST", "/v1/places:searchText", strings.NewReader(`{"textQuery":"x"}`))
	rec := httptest.NewRecorder()
	h.SearchText(rec, req)
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "9200") ||
		!strings.Contains(rec.Body.String(), "Internal error encountered.") {
		t.Fatalf("internal error must be generic: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAutocompleteShape(t *testing.T) {
	h := New(&fakeSearcher{docs: []search.Doc{doc()}}, nil)
	req := httptest.NewRequest("POST", "/v1/places:autocomplete",
		strings.NewReader(`{"input":"ang","languageCode":"en"}`))
	rec := httptest.NewRecorder()
	h.Autocomplete(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	pred := resp["suggestions"].([]any)[0].(map[string]any)["placePrediction"].(map[string]any)
	if pred["placeId"] != "p1" || pred["place"] != "places/p1" {
		t.Fatalf("prediction identity: %v", pred)
	}
	sf := pred["structuredFormat"].(map[string]any)
	if sf["mainText"].(map[string]any)["text"] != "Angkor Wat" {
		t.Fatalf("mainText: %v", sf)
	}
	if sf["secondaryText"].(map[string]any)["text"] != "Siem Reap, Cambodia" {
		t.Fatalf("secondaryText: %v", sf)
	}
}

func TestAutocompleteEmptyInput(t *testing.T) {
	h := New(&fakeSearcher{}, nil)
	req := httptest.NewRequest("POST", "/v1/places:autocomplete", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.Autocomplete(rec, req)
	if rec.Code != 400 {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestAutocompleteEmptyReturnsEmptyArray(t *testing.T) {
	h := New(&fakeSearcher{}, nil)
	req := httptest.NewRequest("POST", "/v1/places:autocomplete",
		strings.NewReader(`{"input":"xyz"}`))
	rec := httptest.NewRecorder()
	h.Autocomplete(rec, req)
	if !strings.Contains(rec.Body.String(), `"suggestions":[]`) {
		t.Fatalf("want suggestions:[], got %s", rec.Body.String())
	}
}

type fakeStore struct{ row *store.PlaceRow }

func (f *fakeStore) GetPlace(_ context.Context, id string) (*store.PlaceRow, error) {
	if f.row != nil && f.row.PlaceID == id {
		return f.row, nil
	}
	return nil, nil
}

func TestGetPlaceDetails(t *testing.T) {
	row := &store.PlaceRow{
		PlaceID:      "p1",
		Names:        map[string]string{"default": "Malis", "km": "ម្លិះ"},
		Categories:   []string{"restaurant"},
		Phone:        "+855 15 814 888",
		Website:      "https://malis.example",
		OpeningHours: "Mo-Su 07:00-22:00",
		Address:      map[string]string{"freeform": "St 123", "locality": "Phnom Penh"},
		Lon:          104.916, Lat: 11.5621,
	}
	h := New(&fakeSearcher{}, &fakeStore{row: row})
	req := httptest.NewRequest("GET", "/v1/places/p1?languageCode=km", nil)
	req.SetPathValue("id", "p1")
	rec := httptest.NewRecorder()
	h.GetPlace(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var place map[string]any
	json.Unmarshal(rec.Body.Bytes(), &place)
	if place["displayName"].(map[string]any)["text"] != "ម្លិះ" {
		t.Fatalf("km name expected: %v", place)
	}
	if place["internationalPhoneNumber"] != "+855 15 814 888" {
		t.Fatalf("phone: %v", place)
	}
	if place["formattedAddress"] != "St 123, Phnom Penh, Cambodia" {
		t.Fatalf("address: %v", place)
	}
	oh := place["regularOpeningHours"].(map[string]any)["weekdayDescriptions"].([]any)
	if oh[0] != "Mo-Su 07:00-22:00" {
		t.Fatalf("hours: %v", oh)
	}
	if place["attributions"].([]any)[0].(map[string]any)["provider"] != "OpenStreetMap contributors" {
		t.Fatalf("attributions missing: %v", place)
	}
}

func TestGetPlaceNotFound(t *testing.T) {
	h := New(&fakeSearcher{}, &fakeStore{})
	req := httptest.NewRequest("GET", "/v1/places/nope", nil)
	req.SetPathValue("id", "nope")
	rec := httptest.NewRecorder()
	h.GetPlace(rec, req)
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), "NOT_FOUND") {
		t.Fatalf("want google-style 404, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestFormatAddressDedupesCambodia(t *testing.T) {
	got := formatAddress(map[string]string{"freeform": "cambodia", "locality": "ភ្នំពេញ"})
	if got != "ភ្នំពេញ, Cambodia" {
		t.Fatalf("dedupe failed: %q", got)
	}
	if got := formatAddress(map[string]string{}); got != "Cambodia" {
		t.Fatalf("empty address: %q", got)
	}
}
