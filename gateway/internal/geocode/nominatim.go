// Nominatim jsonv2 客户端:Search 正向、Reverse 逆向。Reverse 查无结果返回 (nil, nil)。
package geocode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type NominatimResult struct {
	OsmType     string            `json:"osm_type"`
	OsmID       int64             `json:"osm_id"`
	Lat         string            `json:"lat"`
	Lon         string            `json:"lon"`
	Name        string            `json:"name"`
	DisplayName string            `json:"display_name"`
	Addresstype string            `json:"addresstype"`
	Address     map[string]string `json:"address"`
	Error       string            `json:"error"`
}

func (r NominatimResult) Coords() (lat, lon float64, err error) {
	lat, err = strconv.ParseFloat(r.Lat, 64)
	if err != nil {
		return 0, 0, err
	}
	lon, err = strconv.ParseFloat(r.Lon, 64)
	return lat, lon, err
}

type Nominatim struct {
	baseURL string
	http    *http.Client
}

func NewNominatim(baseURL string) *Nominatim {
	return &Nominatim{baseURL: baseURL, http: &http.Client{Timeout: 10 * time.Second}}
}

func (n *Nominatim) get(ctx context.Context, path string, params url.Values, out any) error {
	params.Set("format", "jsonv2")
	params.Set("addressdetails", "1")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		n.baseURL+path+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := n.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("nominatim status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (n *Nominatim) Search(ctx context.Context, q, lang string, limit int) ([]NominatimResult, error) {
	params := url.Values{"q": {q}, "limit": {strconv.Itoa(limit)}, "countrycodes": {"kh"}}
	if lang != "" {
		params.Set("accept-language", lang)
	}
	var out []NominatimResult
	if err := n.get(ctx, "/search", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (n *Nominatim) Reverse(ctx context.Context, lat, lon float64, lang string) (*NominatimResult, error) {
	params := url.Values{
		"lat": {strconv.FormatFloat(lat, 'f', -1, 64)},
		"lon": {strconv.FormatFloat(lon, 'f', -1, 64)},
	}
	if lang != "" {
		params.Set("accept-language", lang)
	}
	var out NominatimResult
	if err := n.get(ctx, "/reverse", params, &out); err != nil {
		return nil, err
	}
	if out.Error != "" { // {"error":"Unable to geocode"}
		return nil, nil
	}
	return &out, nil
}
