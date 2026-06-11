package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

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
	GetNearbyPlaces(ctx context.Context, lat, lon, radiusM float64, limit int) ([]store.PlaceRow, error)
}

// dataAttributions 满足 ODbL/CDLA 的署名义务(spec §11),静态附于每个 Place。
var dataAttributions = []gapi.Attribution{
	{Provider: "OpenStreetMap contributors", ProviderURI: "https://www.openstreetmap.org/copyright"},
	{Provider: "Overture Maps Foundation", ProviderURI: "https://overturemaps.org"},
}

type Handlers struct {
	searcher Searcher
	store    PlaceStore
	geocoder Geocoder
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
	return d.LocalizedName(lang)
}

func docToPlace(d search.Doc, lang string) gapi.Place {
	return gapi.Place{
		Name:             "places/" + d.PlaceID,
		ID:               d.PlaceID,
		DisplayName:      &gapi.LocalizedText{Text: chooseName(d, lang), LanguageCode: lang},
		FormattedAddress: d.FormattedAddress,
		Location:         &gapi.LatLng{Latitude: d.Location.Lat, Longitude: d.Location.Lon},
		Types:            d.Categories,
		Attributions:     dataAttributions,
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

// autocompleteSuggestions 对齐 Google Autocomplete 的建议数上限。
const autocompleteSuggestions = 5

func (h *Handlers) Autocomplete(w http.ResponseWriter, r *http.Request) {
	var req gapi.AutocompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Input == "" {
		invalidArgument(w, "input is required")
		return
	}
	docs, err := h.searcher.Autocomplete(r.Context(), req.Input, geoFromBias(req.LocationBias), autocompleteSuggestions)
	if err != nil {
		internal(w, err)
		return
	}
	resp := gapi.AutocompleteResponse{Suggestions: []gapi.Suggestion{}}
	for _, d := range docs {
		name := chooseName(d, req.LanguageCode)
		resp.Suggestions = append(resp.Suggestions, gapi.Suggestion{PlacePrediction: &gapi.PlacePrediction{
			Place:   "places/" + d.PlaceID,
			PlaceID: d.PlaceID,
			Text:    &gapi.LocalizedText{Text: name},
			StructuredFormat: &gapi.StructuredFormat{
				MainText:      &gapi.LocalizedText{Text: name},
				SecondaryText: &gapi.LocalizedText{Text: d.FormattedAddress},
			},
			Types: d.Categories,
		}})
	}
	writeJSON(w, r, http.StatusOK, resp)
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

func formatAddress(addr map[string]string) string {
	parts := []string{}
	for _, k := range []string{"freeform", "locality", "region"} {
		if v := addr[k]; v != "" && !strings.EqualFold(v, "Cambodia") {
			parts = append(parts, v)
		}
	}
	return strings.Join(append(parts, "Cambodia"), ", ")
}

func (h *Handlers) GetPlace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	row, err := h.store.GetPlace(r.Context(), id)
	if err != nil {
		internal(w, err)
		return
	}
	if row == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "place not found: "+id)
		return
	}
	lang := r.URL.Query().Get("languageCode")
	primary, _, _ := strings.Cut(lang, "-") // BCP-47 → 主语言子标签
	name := row.Names[strings.ToLower(primary)]
	if name == "" {
		name = row.Names["default"]
	}
	place := gapi.Place{
		Name:                     "places/" + row.PlaceID,
		ID:                       row.PlaceID,
		DisplayName:              &gapi.LocalizedText{Text: name, LanguageCode: lang},
		FormattedAddress:         formatAddress(row.Address),
		Location:                 &gapi.LatLng{Latitude: row.Lat, Longitude: row.Lon},
		Types:                    row.Categories,
		InternationalPhoneNumber: row.Phone,
		WebsiteURI:               row.Website,
		Attributions:             dataAttributions,
	}
	if row.OpeningHours != "" {
		// 简化:OSM opening_hours 原文作为单条 weekdayDescriptions,不解析为 periods(见 spec §4)
		place.RegularOpeningHours = &gapi.OpeningHours{WeekdayDescriptions: []string{row.OpeningHours}}
	}
	writeJSON(w, r, http.StatusOK, place)
}
