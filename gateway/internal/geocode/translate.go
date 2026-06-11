// 把 Nominatim 结果 / OpenSearch Doc 翻译成 legacy Geocoding 形态。
package geocode

import (
	"strconv"
	"strings"

	"github.com/danielnanuk/open-map-service/gateway/internal/gapi"
	"github.com/danielnanuk/open-map-service/gateway/internal/search"
)

// Nominatim address 键 → Google address_components 类型
var componentTypes = []struct {
	key    string
	gtypes []string
}{
	{"house_number", []string{"street_number"}},
	{"road", []string{"route"}},
	{"neighbourhood", []string{"sublocality", "political"}},
	{"quarter", []string{"sublocality", "political"}},
	{"suburb", []string{"sublocality", "political"}},
	{"village", []string{"locality", "political"}},
	{"town", []string{"locality", "political"}},
	{"city", []string{"locality", "political"}},
	{"county", []string{"administrative_area_level_2", "political"}},
	{"state", []string{"administrative_area_level_1", "political"}},
	{"postcode", []string{"postal_code"}},
	{"country", []string{"country", "political"}},
}

func NominatimToGeocodeResult(n NominatimResult) (gapi.GeocodeResult, error) {
	lat, lon, err := n.Coords()
	if err != nil {
		return gapi.GeocodeResult{}, err
	}
	comps := []gapi.AddressComponent{}
	for _, ct := range componentTypes {
		v := n.Address[ct.key]
		if v == "" {
			continue
		}
		short := v
		if ct.key == "country" {
			short = strings.ToUpper(n.Address["country_code"])
		}
		comps = append(comps, gapi.AddressComponent{LongName: v, ShortName: short, Types: ct.gtypes})
	}
	locType := "APPROXIMATE"
	rtypes := []string{"geocode"}
	switch {
	case n.Address["house_number"] != "":
		locType, rtypes = "ROOFTOP", []string{"street_address"}
	case n.Address["road"] != "":
		locType, rtypes = "GEOMETRIC_CENTER", []string{"route"}
	case n.Address["city"] != "" || n.Address["town"] != "" || n.Address["village"] != "":
		rtypes = []string{"locality", "political"}
	case n.Address["state"] != "":
		rtypes = []string{"administrative_area_level_1", "political"}
	}
	return gapi.GeocodeResult{
		AddressComponents: comps,
		FormattedAddress:  n.DisplayName,
		Geometry: gapi.GeocodeGeometry{
			Location:     gapi.GeoLatLng{Lat: lat, Lng: lon},
			LocationType: locType,
		},
		PlaceID: "nominatim:" + n.OsmType + ":" + strconv.FormatInt(n.OsmID, 10),
		Types:   rtypes,
	}, nil
}

func DocToGeocodeResult(d search.Doc) gapi.GeocodeResult {
	return gapi.GeocodeResult{
		AddressComponents: []gapi.AddressComponent{},
		FormattedAddress:  d.NameDefault + ", " + d.FormattedAddress,
		Geometry: gapi.GeocodeGeometry{
			Location:     gapi.GeoLatLng{Lat: d.Location.Lat, Lng: d.Location.Lon},
			LocationType: "GEOMETRIC_CENTER",
		},
		PlaceID: d.PlaceID,
		Types:   d.Categories,
	}
}
