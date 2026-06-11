package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielnanuk/open-map-service/gateway/internal/route"
)

type fakeRouter struct {
	trip       *route.Trip
	err        error
	gotCosting string
	gotOpts    map[string]any
	gotLang    string
}

func (f *fakeRouter) Route(_ context.Context, locs []route.Location, costing string,
	opts map[string]any, lang string) (*route.Trip, error) {
	f.gotCosting, f.gotOpts, f.gotLang = costing, opts, lang
	return f.trip, f.err
}

func sampleTrip() *route.Trip {
	shape := route.EncodePolyline6([][2]float64{{11.5564, 104.9282}, {11.5696, 104.9210}})
	return &route.Trip{
		Legs:    []route.Leg{{Shape: shape, Summary: route.LegSummary{Time: 620.5, Length: 2.41}}},
		Summary: route.LegSummary{Time: 620.5, Length: 2.41},
	}
}

func routesPOST(t *testing.T, h *Handlers, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("POST", "/directions/v2:computeRoutes", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ComputeRoutes(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

const validRoutesBody = `{"origin":{"location":{"latLng":{"latitude":11.5564,"longitude":104.9282}}},
 "destination":{"location":{"latLng":{"latitude":11.5696,"longitude":104.9210}}},
 "travelMode":"TWO_WHEELER","vehicleProfile":"tuktuk","languageCode":"km"}`

func TestComputeRoutesShape(t *testing.T) {
	fr := &fakeRouter{trip: sampleTrip()}
	h := NewWithGeocoder(&fakeSearcher{}, nil, &fakeGeocoder{}).WithRouter(fr)
	code, out := routesPOST(t, h, validRoutesBody)
	if code != 200 {
		t.Fatalf("status %d: %v", code, out)
	}
	r0 := out["routes"].([]any)[0].(map[string]any)
	if r0["distanceMeters"].(float64) != 2410 {
		t.Fatalf("distance: %v", r0["distanceMeters"])
	}
	if r0["duration"] != "621s" { // round(620.5)
		t.Fatalf("duration: %v", r0["duration"])
	}
	if r0["polyline"].(map[string]any)["encodedPolyline"] == "" {
		t.Fatal("polyline empty")
	}
	if len(r0["legs"].([]any)) != 1 {
		t.Fatalf("legs: %v", r0["legs"])
	}
	if fr.gotCosting != "motor_scooter" || fr.gotLang != "km" {
		t.Fatalf("router args: %s %s", fr.gotCosting, fr.gotLang)
	}
	if _, ok := fr.gotOpts["motor_scooter"]; !ok {
		t.Fatalf("tuktuk options not passed: %v", fr.gotOpts)
	}
}

func TestComputeRoutesMissingOrigin(t *testing.T) {
	h := NewWithGeocoder(&fakeSearcher{}, nil, &fakeGeocoder{}).WithRouter(&fakeRouter{})
	code, out := routesPOST(t, h, `{"destination":{"location":{"latLng":{"latitude":1,"longitude":2}}}}`)
	raw, _ := json.Marshal(out)
	if code != 400 || !strings.Contains(string(raw), "INVALID_ARGUMENT") {
		t.Fatalf("%d %v", code, out)
	}
}

func TestComputeRoutesUnknownMode(t *testing.T) {
	h := NewWithGeocoder(&fakeSearcher{}, nil, &fakeGeocoder{}).WithRouter(&fakeRouter{})
	code, _ := routesPOST(t, h, strings.Replace(validRoutesBody, "TWO_WHEELER", "TRANSIT", 1))
	if code != 400 {
		t.Fatalf("want 400, got %d", code)
	}
}

func TestComputeRoutesNoPathIsEmptyRoutes(t *testing.T) {
	h := NewWithGeocoder(&fakeSearcher{}, nil, &fakeGeocoder{}).WithRouter(&fakeRouter{trip: nil})
	code, out := routesPOST(t, h, validRoutesBody)
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	if routes, ok := out["routes"].([]any); !ok || len(routes) != 0 {
		t.Fatalf("want empty routes array: %v", out)
	}
}

func TestComputeRoutesBadJSONAndNilRouter(t *testing.T) {
	h := NewWithGeocoder(&fakeSearcher{}, nil, &fakeGeocoder{}).WithRouter(&fakeRouter{})
	code, _ := routesPOST(t, h, `{bad json`)
	if code != 400 {
		t.Fatalf("bad json: want 400, got %d", code)
	}
	hNil := NewWithGeocoder(&fakeSearcher{}, nil, &fakeGeocoder{}) // 未装配 router
	code, _ = routesPOST(t, hNil, validRoutesBody)
	if code != 500 {
		t.Fatalf("nil router: want clean 500, got %d", code)
	}
}
