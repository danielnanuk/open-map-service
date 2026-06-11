package gapi

// computeRouteMatrix:请求 origins×destinations,响应为元素数组(非对象)。
type MatrixWaypoint struct {
	Waypoint RouteWaypoint `json:"waypoint"`
}

type ComputeRouteMatrixRequest struct {
	Origins        []MatrixWaypoint `json:"origins"`
	Destinations   []MatrixWaypoint `json:"destinations"`
	TravelMode     string           `json:"travelMode,omitempty"`
	VehicleProfile string           `json:"vehicleProfile,omitempty"` // 协议扩展
}

type MatrixElement struct {
	OriginIndex      int    `json:"originIndex"`
	DestinationIndex int    `json:"destinationIndex"`
	DistanceMeters   int    `json:"distanceMeters,omitempty"`
	Duration         string `json:"duration,omitempty"` // "123s"
	Condition        string `json:"condition"`          // ROUTE_EXISTS | ROUTE_NOT_FOUND
}
