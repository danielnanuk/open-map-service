// POST /distanceMatrix/v2:computeRouteMatrix —— 响应为元素数组。
// 分流:元素数 > matrixThreshold 走对应 profile 的 OSRM;否则(或 OSRM 失败/无实例)走 Valhalla(自动分块)。
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"

	"github.com/danielnanuk/open-map-service/gateway/internal/gapi"
	"github.com/danielnanuk/open-map-service/gateway/internal/metrics"
	"github.com/danielnanuk/open-map-service/gateway/internal/route"
)

const (
	matrixThreshold    = 2500 // 50×50:Valhalla 单次位置上限的平方
	matrixMaxLocations = 1000 // 单侧上限:控制 URL/响应体规模(实测 v26 不强制 max-table-size,此处自限)
)

type MatrixRouter interface {
	Matrix(ctx context.Context, sources, targets []route.Location,
		costing string, costingOptions map[string]any) ([][]*float64, [][]*float64, error)
}

// WithMatrix 装配:per-costing 的 OSRM 实例表 + Valhalla 兜底。
func (h *Handlers) WithMatrix(routers map[string]MatrixRouter, fallback MatrixRouter) *Handlers {
	h.matrixRouters = routers
	h.matrixFallback = fallback
	return h
}

// matrixKey:OSRM 实例选择键。tuktuk 用独立图,其余按 costing。
func matrixKey(costing, vehicleProfile string) string {
	if vehicleProfile == "tuktuk" {
		return "tuktuk"
	}
	return costing
}

func waypointsToLocations(wps []gapi.MatrixWaypoint) ([]route.Location, bool) {
	out := make([]route.Location, len(wps))
	for i, wp := range wps {
		loc, ok := waypointToLocation(wp.Waypoint)
		if !ok {
			return nil, false
		}
		out[i] = loc
	}
	return out, true
}

func (h *Handlers) ComputeRouteMatrix(w http.ResponseWriter, r *http.Request) {
	if h.matrixFallback == nil { // 未经 WithMatrix 装配:干净 500
		internal(w, fmt.Errorf("matrix not configured"))
		return
	}
	var req gapi.ComputeRouteMatrixRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		invalidArgument(w, "invalid request body")
		return
	}
	if len(req.Origins) == 0 || len(req.Destinations) == 0 {
		invalidArgument(w, "origins and destinations are required")
		return
	}
	if len(req.Origins) > matrixMaxLocations || len(req.Destinations) > matrixMaxLocations {
		invalidArgument(w, fmt.Sprintf("at most %d origins/destinations", matrixMaxLocations))
		return
	}
	origins, ok1 := waypointsToLocations(req.Origins)
	dests, ok2 := waypointsToLocations(req.Destinations)
	if !ok1 || !ok2 {
		invalidArgument(w, "every waypoint needs location.latLng")
		return
	}
	costing, opts, err := route.TravelModeToCosting(req.TravelMode, req.VehicleProfile)
	if err != nil {
		invalidArgument(w, err.Error())
		return
	}

	var durs, dists [][]*float64
	elements := len(origins) * len(dests)
	if inst, has := h.matrixRouters[matrixKey(costing, req.VehicleProfile)]; has && elements > matrixThreshold {
		durs, dists, err = inst.Matrix(r.Context(), origins, dests, costing, opts)
		if err != nil {
			log.Printf("matrix osrm failed, falling back to valhalla: %v", err)
			metrics.BackendErrors.WithLabelValues("osrm").Inc()
			durs, dists, err = h.matrixFallback.Matrix(r.Context(), origins, dests, costing, opts)
		}
	} else {
		durs, dists, err = h.matrixFallback.Matrix(r.Context(), origins, dests, costing, opts)
	}
	if err != nil {
		internal(w, err)
		return
	}

	out := make([]gapi.MatrixElement, 0, elements)
	for i := range durs {
		for j := range durs[i] {
			el := gapi.MatrixElement{OriginIndex: i, DestinationIndex: j, Condition: "ROUTE_NOT_FOUND"}
			if durs[i][j] != nil {
				el.Condition = "ROUTE_EXISTS"
				el.Duration = fmt.Sprintf("%ds", int(math.Round(*durs[i][j])))
				if dists[i][j] != nil {
					el.DistanceMeters = int(math.Round(*dists[i][j]))
				}
			}
			out = append(out, el)
		}
	}
	writeJSON(w, r, http.StatusOK, out)
}
