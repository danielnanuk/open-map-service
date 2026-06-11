// GET /maps/api/geocode/json —— legacy Geocoding 协议:HTTP 恒 200,状态在 body.status。
package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielnanuk/open-map-service/gateway/internal/gapi"
	"github.com/danielnanuk/open-map-service/gateway/internal/geocode"
	"github.com/danielnanuk/open-map-service/gateway/internal/metrics"
	"github.com/danielnanuk/open-map-service/gateway/internal/store"
)

type Geocoder interface {
	Search(ctx context.Context, q, lang string, limit int) ([]geocode.NominatimResult, error)
	Reverse(ctx context.Context, lat, lon float64, lang string) (*geocode.NominatimResult, error)
}

// NewWithGeocoder 在 New 基础上挂接 Nominatim。
func NewWithGeocoder(s Searcher, ps PlaceStore, g Geocoder) *Handlers {
	h := New(s, ps)
	h.geocoder = g
	return h
}

const geocodeLimit = 5

func writeGeocode(w http.ResponseWriter, results []gapi.GeocodeResult, status string) {
	if results == nil {
		results = []gapi.GeocodeResult{}
	}
	if status == "" {
		status = "OK"
		if len(results) == 0 {
			status = "ZERO_RESULTS"
		}
	}
	if status == "ZERO_RESULTS" {
		metrics.ZeroResults.WithLabelValues("/maps/api/geocode/json").Inc()
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(gapi.GeocodeResponse{Results: results, Status: status})
}

func (h *Handlers) Geocode(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lang := q.Get("language")
	switch {
	case q.Get("latlng") != "":
		h.reverseGeocode(w, r, q.Get("latlng"), lang)
	case q.Get("address") != "":
		h.forwardGeocode(w, r, q.Get("address"), lang)
	default:
		writeGeocode(w, nil, "INVALID_REQUEST")
	}
}

const (
	reversePOIRadiusM = 150
	reversePOILimit   = 4
)

func (h *Handlers) reverseGeocode(w http.ResponseWriter, r *http.Request, latlng, lang string) {
	parts := strings.Split(latlng, ",")
	if len(parts) != 2 {
		writeGeocode(w, nil, "INVALID_REQUEST")
		return
	}
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lon, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil || math.IsNaN(lat) || math.IsNaN(lon) ||
		lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		writeGeocode(w, nil, "INVALID_REQUEST")
		return
	}
	ctx := r.Context()
	var results []gapi.GeocodeResult
	if n, err := h.geocoder.Reverse(ctx, lat, lon, lang); err != nil {
		log.Printf("reverse nominatim: %v", err)
		metrics.BackendErrors.WithLabelValues("nominatim").Inc()
	} else if n != nil {
		if g, err := geocode.NominatimToGeocodeResult(*n); err == nil {
			results = append(results, g)
		}
	}
	if rows, err := h.store.GetNearbyPlaces(ctx, lat, lon, reversePOIRadiusM, reversePOILimit); err != nil {
		log.Printf("reverse nearby: %v", err)
		metrics.BackendErrors.WithLabelValues("postgis").Inc()
	} else {
		for _, p := range rows {
			results = append(results, placeRowToGeocodeResult(p, lang))
		}
	}
	// 双源都失败时静默呈现 ZERO_RESULTS(不是 UNKNOWN_ERROR):逆向两个来源异构,
	// 海面点的 store 错误与真零结果无法区分,surface 错误只会造成误报
	writeGeocode(w, results, "")
}

func placeRowToGeocodeResult(p store.PlaceRow, lang string) gapi.GeocodeResult {
	primary, _, _ := strings.Cut(lang, "-")
	name := p.Names[strings.ToLower(primary)]
	if name == "" {
		name = p.Names["default"]
	}
	return gapi.GeocodeResult{
		AddressComponents: []gapi.AddressComponent{},
		FormattedAddress:  name + ", " + formatAddress(p.Address),
		Geometry: gapi.GeocodeGeometry{
			Location:     gapi.GeoLatLng{Lat: p.Lat, Lng: p.Lon},
			LocationType: "GEOMETRIC_CENTER",
		},
		PlaceID: p.PlaceID,
		Types:   p.Categories,
	}
}

func (h *Handlers) forwardGeocode(w http.ResponseWriter, r *http.Request, address, lang string) {
	ctx := r.Context()
	nominatimFirst := geocode.AddressLike(address)
	var results []gapi.GeocodeResult
	var errCount int

	tryNominatim := func() {
		res, err := h.geocoder.Search(ctx, address, lang, geocodeLimit)
		if err != nil {
			log.Printf("geocode nominatim: %v", err)
			metrics.BackendErrors.WithLabelValues("nominatim").Inc()
			errCount++
			return
		}
		for _, n := range res {
			// 坏坐标的单条结果静默跳过(translation 失败不计入 errCount):宁可少一条也不整批失败
			if g, err := geocode.NominatimToGeocodeResult(n); err == nil {
				results = append(results, g)
			}
		}
	}
	tryOpenSearch := func() {
		docs, err := h.searcher.SearchText(ctx, address, nil, geocodeLimit)
		if err != nil {
			log.Printf("geocode opensearch: %v", err)
			metrics.BackendErrors.WithLabelValues("opensearch").Inc()
			errCount++
			return
		}
		for _, d := range docs {
			results = append(results, geocode.DocToGeocodeResult(d, lang))
		}
	}

	if nominatimFirst {
		tryNominatim()
		if len(results) == 0 {
			tryOpenSearch()
		}
	} else {
		tryOpenSearch()
		if len(results) == 0 {
			tryNominatim()
		}
	}
	if len(results) == 0 && errCount == 2 {
		writeGeocode(w, nil, "UNKNOWN_ERROR")
		return
	}
	writeGeocode(w, results, "")
}
