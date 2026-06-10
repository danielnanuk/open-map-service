package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/danielnanuk/open-map-service/gateway/internal/gapi"
	"github.com/danielnanuk/open-map-service/gateway/internal/search"
	"github.com/danielnanuk/open-map-service/gateway/internal/store"
)

type Searcher interface {
	SearchText(ctx context.Context, q string, bias *search.Geo, size int) ([]search.Doc, error)
	SearchNearby(ctx context.Context, center search.Geo, radius float64, types []string, size int, byDistance bool) ([]search.Doc, error)
	Autocomplete(ctx context.Context, input string, bias *search.Geo, size int) ([]search.Doc, error)
}

type PlaceStore interface {
	GetPlace(ctx context.Context, id string) (*store.PlaceRow, error)
}

type Handlers struct {
	searcher Searcher
	store    PlaceStore
}

func New(s Searcher, ps PlaceStore) *Handlers {
	return &Handlers{searcher: s, store: ps}
}

func geoFromBias(b *gapi.Bias) *search.Geo {
	if b == nil || b.Circle == nil {
		return nil
	}
	return &search.Geo{Lat: b.Circle.Center.Latitude, Lon: b.Circle.Center.Longitude}
}

// chooseName 按 languageCode 选展示名,缺失回退 default。
func chooseName(d search.Doc, lang string) string {
	switch lang {
	case "km":
		if d.NameKm != "" {
			return d.NameKm
		}
	case "zh":
		if d.NameZh != "" {
			return d.NameZh
		}
	case "en":
		if d.NameEn != "" {
			return d.NameEn
		}
	}
	return d.NameDefault
}

func docToPlace(d search.Doc, lang string) gapi.Place {
	return gapi.Place{
		Name:             "places/" + d.PlaceID,
		ID:               d.PlaceID,
		DisplayName:      &gapi.LocalizedText{Text: chooseName(d, lang), LanguageCode: lang},
		FormattedAddress: d.FormattedAddress,
		Location:         &gapi.LatLng{Latitude: d.Location.Lat, Longitude: d.Location.Lon},
		Types:            d.Categories,
	}
}

func docsToPlacesResponse(docs []search.Doc, lang string) gapi.PlacesResponse {
	out := gapi.PlacesResponse{Places: []gapi.Place{}}
	for _, d := range docs {
		out.Places = append(out.Places, docToPlace(d, lang))
	}
	return out
}

func (h *Handlers) SearchText(w http.ResponseWriter, r *http.Request) {
	var req gapi.SearchTextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TextQuery == "" {
		invalidArgument(w, "textQuery is required")
		return
	}
	size := req.PageSize
	if size <= 0 || size > 20 {
		size = 10
	}
	docs, err := h.searcher.SearchText(r.Context(), req.TextQuery, geoFromBias(req.LocationBias), size)
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, r, http.StatusOK, docsToPlacesResponse(docs, req.LanguageCode))
}

func (h *Handlers) SearchNearby(w http.ResponseWriter, r *http.Request) {
	var req gapi.SearchNearbyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LocationRestriction.Circle == nil {
		invalidArgument(w, "locationRestriction.circle is required")
		return
	}
	c := req.LocationRestriction.Circle
	if c.Radius <= 0 || c.Radius > 50000 {
		invalidArgument(w, "radius must be in (0, 50000]")
		return
	}
	size := req.MaxResultCount
	if size <= 0 || size > 20 {
		size = 20
	}
	docs, err := h.searcher.SearchNearby(r.Context(),
		search.Geo{Lat: c.Center.Latitude, Lon: c.Center.Longitude},
		c.Radius, req.IncludedTypes, size, req.RankPreference == "DISTANCE")
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, r, http.StatusOK, docsToPlacesResponse(docs, req.LanguageCode))
}
