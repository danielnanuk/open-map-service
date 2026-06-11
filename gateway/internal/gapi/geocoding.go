package gapi

// legacy Geocoding API 的响应形态:HTTP 恒 200,错误用顶层 status 表达。
// 注意坐标键是 lat/lng(legacy),不是 Places New 的 latitude/longitude。
type GeoLatLng struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type AddressComponent struct {
	LongName  string   `json:"long_name"`
	ShortName string   `json:"short_name"`
	Types     []string `json:"types"`
}

type GeocodeGeometry struct {
	Location     GeoLatLng `json:"location"`
	LocationType string    `json:"location_type"` // ROOFTOP | GEOMETRIC_CENTER | APPROXIMATE
}

type GeocodeResult struct {
	AddressComponents []AddressComponent `json:"address_components"`
	FormattedAddress  string             `json:"formatted_address"`
	Geometry          GeocodeGeometry    `json:"geometry"`
	PlaceID           string             `json:"place_id"`
	Types             []string           `json:"types"`
}

type GeocodeResponse struct {
	Results []GeocodeResult `json:"results"`
	Status  string          `json:"status"` // OK | ZERO_RESULTS | INVALID_REQUEST | UNKNOWN_ERROR
}
