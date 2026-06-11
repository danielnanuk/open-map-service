// GET /maps/api/geocode/json —— legacy Geocoding 协议:HTTP 恒 200,状态在 body.status。
package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/danielnanuk/open-map-service/gateway/internal/gapi"
	"github.com/danielnanuk/open-map-service/gateway/internal/geocode"
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

// reverseGeocode 在 T7 实现;占位对 latlng 请求返回 INVALID_REQUEST(T7 替换并带测试)。
func (h *Handlers) reverseGeocode(w http.ResponseWriter, r *http.Request, latlng, lang string) {
	writeGeocode(w, nil, "INVALID_REQUEST")
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
