package gapi

// Routes API v2(computeRoutes)的请求/响应子集。
type RouteLocation struct {
	LatLng LatLng `json:"latLng"`
}

type RouteWaypoint struct {
	Location *RouteLocation `json:"location,omitempty"`
}

type ComputeRoutesRequest struct {
	Origin       RouteWaypoint `json:"origin"`
	Destination  RouteWaypoint `json:"destination"`
	TravelMode   string        `json:"travelMode,omitempty"` // DRIVE|TWO_WHEELER|WALK|BICYCLE
	LanguageCode string        `json:"languageCode,omitempty"`
	// 协议扩展(Google 没有嘟嘟车):travelMode=TWO_WHEELER + vehicleProfile="tuktuk"
	VehicleProfile string `json:"vehicleProfile,omitempty"`
}

type RoutePolyline struct {
	EncodedPolyline string `json:"encodedPolyline"`
}

type RouteLeg struct {
	DistanceMeters int           `json:"distanceMeters"`
	Duration       string        `json:"duration"` // "1234s"
	Polyline       RoutePolyline `json:"polyline"`
}

type Route struct {
	DistanceMeters int           `json:"distanceMeters"`
	Duration       string        `json:"duration"`
	Polyline       RoutePolyline `json:"polyline"`
	Legs           []RouteLeg    `json:"legs"`
}

type ComputeRoutesResponse struct {
	Routes []Route `json:"routes"`
}
