package geocode

import (
	"testing"

	"github.com/danielnanuk/open-map-service/gateway/internal/search"
)

func TestNominatimToGeocodeResult(t *testing.T) {
	n := NominatimResult{
		OsmType: "way", OsmID: 456, Lat: "11.5563", Lon: "104.9282",
		DisplayName: "271, Street 271, Phnom Penh, 12311, Cambodia",
		Addresstype: "road",
		Address: map[string]string{
			"house_number": "271", "road": "Street 271", "city": "Phnom Penh",
			"postcode": "12311", "country": "Cambodia", "country_code": "kh",
		},
	}
	g, err := NominatimToGeocodeResult(n)
	if err != nil {
		t.Fatal(err)
	}
	if g.PlaceID != "nominatim:way:456" {
		t.Fatalf("place_id: %s", g.PlaceID)
	}
	if g.Geometry.Location.Lat != 11.5563 || g.Geometry.Location.Lng != 104.9282 {
		t.Fatalf("location: %+v", g.Geometry.Location)
	}
	if g.Geometry.LocationType != "ROOFTOP" { // 有 house_number
		t.Fatalf("location_type: %s", g.Geometry.LocationType)
	}
	comp := map[string]string{}
	for _, c := range g.AddressComponents {
		comp[c.Types[0]] = c.LongName
	}
	if comp["street_number"] != "271" || comp["route"] != "Street 271" ||
		comp["locality"] != "Phnom Penh" || comp["postal_code"] != "12311" {
		t.Fatalf("components: %v", comp)
	}
	for _, c := range g.AddressComponents {
		if c.Types[0] == "country" && c.ShortName != "KH" {
			t.Fatalf("country short: %v", c)
		}
	}
	if g.Types[0] != "street_address" {
		t.Fatalf("types: %v", g.Types)
	}
}

func TestNominatimLocationTypeFallbacks(t *testing.T) {
	road := NominatimResult{Lat: "1", Lon: "2", Address: map[string]string{"road": "X"}}
	g, _ := NominatimToGeocodeResult(road)
	if g.Geometry.LocationType != "GEOMETRIC_CENTER" || g.Types[0] != "route" {
		t.Fatalf("road-level: %+v", g)
	}
	admin := NominatimResult{Lat: "1", Lon: "2", Address: map[string]string{"state": "Siem Reap"}}
	g, _ = NominatimToGeocodeResult(admin)
	if g.Geometry.LocationType != "APPROXIMATE" || g.Types[0] != "administrative_area_level_1" {
		t.Fatalf("admin-level: %+v", g)
	}
}

func TestNominatimBadCoordsSurface(t *testing.T) {
	if _, err := NominatimToGeocodeResult(NominatimResult{Lat: "x", Lon: "y"}); err == nil {
		t.Fatal("want error on unparseable coords")
	}
}

func TestNominatimProvinceKeyMapsToAdmin1(t *testing.T) {
	// 实测:西哈努克等省 Nominatim 返回 province 键而非 state
	n := NominatimResult{Lat: "10.6", Lon: "103.5",
		Address: map[string]string{"province": "ខេត្តព្រះសីហនុ", "country": "Cambodia", "country_code": "kh"}}
	g, err := NominatimToGeocodeResult(n)
	if err != nil {
		t.Fatal(err)
	}
	if g.Types[0] != "administrative_area_level_1" {
		t.Fatalf("types: %v", g.Types)
	}
	found := false
	for _, c := range g.AddressComponents {
		if c.Types[0] == "administrative_area_level_1" && c.LongName == "ខេត្តព្រះសីហនុ" {
			found = true
		}
	}
	if !found {
		t.Fatalf("province component missing: %+v", g.AddressComponents)
	}
}

func TestDocToGeocodeResult(t *testing.T) {
	d := search.Doc{PlaceID: "p1", NameDefault: "Angkor Wat",
		FormattedAddress: "Siem Reap, Cambodia", Categories: []string{"tourist_attraction"}}
	d.Location.Lat, d.Location.Lon = 13.4125, 103.867
	g := DocToGeocodeResult(d)
	if g.PlaceID != "p1" || g.Geometry.Location.Lat != 13.4125 {
		t.Fatalf("%+v", g)
	}
	if g.FormattedAddress != "Angkor Wat, Siem Reap, Cambodia" {
		t.Fatalf("formatted: %s", g.FormattedAddress)
	}
	if g.Geometry.LocationType != "GEOMETRIC_CENTER" || g.Types[0] != "tourist_attraction" {
		t.Fatalf("%+v", g)
	}
}
