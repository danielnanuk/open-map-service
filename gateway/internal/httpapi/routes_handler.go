// POST /directions/v2:computeRoutes —— Routes API v2(新版协议:HTTP 状态码 + Google 错误体)。
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"

	"github.com/danielnanuk/open-map-service/gateway/internal/gapi"
	"github.com/danielnanuk/open-map-service/gateway/internal/route"
)

type Router interface {
	Route(ctx context.Context, locs []route.Location, costing string,
		opts map[string]any, lang string) (*route.Trip, error)
}

// WithRouter 链式挂接 Valhalla。
func (h *Handlers) WithRouter(r Router) *Handlers {
	h.router = r
	return h
}

func waypointToLocation(wp gapi.RouteWaypoint) (route.Location, bool) {
	if wp.Location == nil {
		return route.Location{}, false
	}
	ll := wp.Location.LatLng
	return route.Location{Lat: ll.Latitude, Lon: ll.Longitude}, true
}

func tripToRoute(t *route.Trip) gapi.Route {
	r := gapi.Route{
		DistanceMeters: int(math.Round(t.Summary.Length * 1000)),
		Duration:       fmt.Sprintf("%ds", int(math.Round(t.Summary.Time))),
		Legs:           []gapi.RouteLeg{},
	}
	var full string
	for _, leg := range t.Legs {
		p5 := route.Polyline6To5(leg.Shape)
		full = p5 // M3 仅两点请求,恒单段;多段拼接留给 waypoints 支持时处理
		r.Legs = append(r.Legs, gapi.RouteLeg{
			DistanceMeters: int(math.Round(leg.Summary.Length * 1000)),
			Duration:       fmt.Sprintf("%ds", int(math.Round(leg.Summary.Time))),
			Polyline:       gapi.RoutePolyline{EncodedPolyline: p5},
		})
	}
	r.Polyline = gapi.RoutePolyline{EncodedPolyline: full}
	return r
}

func (h *Handlers) ComputeRoutes(w http.ResponseWriter, r *http.Request) {
	if h.router == nil { // 未经 WithRouter 装配:干净 500 而不是 nil panic
		internal(w, fmt.Errorf("router not configured"))
		return
	}
	var req gapi.ComputeRoutesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		invalidArgument(w, "invalid request body")
		return
	}
	origin, ok1 := waypointToLocation(req.Origin)
	dest, ok2 := waypointToLocation(req.Destination)
	if !ok1 || !ok2 {
		invalidArgument(w, "origin.location and destination.location are required")
		return
	}
	costing, opts, err := route.TravelModeToCosting(req.TravelMode, req.VehicleProfile)
	if err != nil {
		invalidArgument(w, err.Error())
		return
	}
	trip, err := h.router.Route(r.Context(), []route.Location{origin, dest}, costing, opts, req.LanguageCode)
	if err != nil {
		internal(w, err)
		return
	}
	resp := gapi.ComputeRoutesResponse{Routes: []gapi.Route{}}
	if trip != nil && len(trip.Legs) > 0 {
		resp.Routes = append(resp.Routes, tripToRoute(trip))
	}
	writeJSON(w, r, http.StatusOK, resp)
}
