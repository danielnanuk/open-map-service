package gapi

type LatLng struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type Circle struct {
	Center LatLng  `json:"center"`
	Radius float64 `json:"radius"`
}

type Bias struct {
	Circle *Circle `json:"circle,omitempty"`
}

type LocalizedText struct {
	Text         string `json:"text"`
	LanguageCode string `json:"languageCode,omitempty"`
}

type OpeningHours struct {
	WeekdayDescriptions []string `json:"weekdayDescriptions,omitempty"`
}

type Place struct {
	Name                     string         `json:"name,omitempty"` // "places/<id>"
	ID                       string         `json:"id,omitempty"`
	DisplayName              *LocalizedText `json:"displayName,omitempty"`
	FormattedAddress         string         `json:"formattedAddress,omitempty"`
	Location                 *LatLng        `json:"location,omitempty"`
	Types                    []string       `json:"types,omitempty"`
	InternationalPhoneNumber string         `json:"internationalPhoneNumber,omitempty"`
	WebsiteURI               string         `json:"websiteUri,omitempty"`
	RegularOpeningHours      *OpeningHours  `json:"regularOpeningHours,omitempty"`
}

type SearchTextRequest struct {
	TextQuery    string `json:"textQuery"`
	LanguageCode string `json:"languageCode,omitempty"`
	PageSize     int    `json:"pageSize,omitempty"`
	LocationBias *Bias  `json:"locationBias,omitempty"`
}

type SearchNearbyRequest struct {
	LocationRestriction Bias     `json:"locationRestriction"` // required;有意非指针,勿改成 *Bias
	IncludedTypes       []string `json:"includedTypes,omitempty"`
	MaxResultCount      int      `json:"maxResultCount,omitempty"`
	RankPreference      string   `json:"rankPreference,omitempty"` // POPULARITY | DISTANCE
	LanguageCode        string   `json:"languageCode,omitempty"`
}

type PlacesResponse struct {
	Places []Place `json:"places"`
}

type AutocompleteRequest struct {
	Input        string `json:"input"`
	LanguageCode string `json:"languageCode,omitempty"`
	LocationBias *Bias  `json:"locationBias,omitempty"`
}

type StructuredFormat struct {
	MainText      *LocalizedText `json:"mainText,omitempty"`
	SecondaryText *LocalizedText `json:"secondaryText,omitempty"`
}

type PlacePrediction struct {
	Place            string            `json:"place"` // "places/<id>"
	PlaceID          string            `json:"placeId"`
	Text             *LocalizedText    `json:"text,omitempty"`
	StructuredFormat *StructuredFormat `json:"structuredFormat,omitempty"`
	Types            []string          `json:"types,omitempty"`
}

type Suggestion struct {
	PlacePrediction *PlacePrediction `json:"placePrediction,omitempty"`
}

type AutocompleteResponse struct {
	Suggestions []Suggestion `json:"suggestions"`
}

// Google 风格错误体:{"error":{"code":400,"message":"...","status":"INVALID_ARGUMENT"}}
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}
