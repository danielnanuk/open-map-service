package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Doc 与 etl/etl/index.py 写入的 _source 字段一一对应。
type Doc struct {
	PlaceID          string   `json:"place_id"`
	NameDefault      string   `json:"name_default"`
	NameEn           string   `json:"name_en"`
	NameKm           string   `json:"name_km"`
	NameZh           string   `json:"name_zh"`
	FormattedAddress string   `json:"formatted_address"`
	Categories       []string `json:"categories"`
	Confidence       float64  `json:"confidence"`
	Location         struct {
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
	} `json:"location"`
}

// LocalizedName 按 BCP-47 语言码选展示名,缺失回退 name_default。
func (d Doc) LocalizedName(lang string) string {
	primary, _, _ := strings.Cut(lang, "-")
	switch strings.ToLower(primary) {
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

type Client struct {
	BaseURL string
	Index   string
	HTTP    *http.Client
}

func New(baseURL string) *Client {
	return &Client{BaseURL: baseURL, Index: "places",
		HTTP: &http.Client{Timeout: 10 * time.Second}}
}

func (c *Client) SearchText(ctx context.Context, q string, bias *Geo, size int) ([]Doc, error) {
	return c.run(ctx, searchTextBody(q, bias, size))
}

func (c *Client) SearchNearby(ctx context.Context, center Geo, radius float64, types []string, size int, byDistance bool) ([]Doc, error) {
	return c.run(ctx, nearbyBody(center, radius, types, size, byDistance))
}

func (c *Client) Autocomplete(ctx context.Context, input string, bias *Geo, size int) ([]Doc, error) {
	return c.run(ctx, autocompleteBody(input, bias, size))
}

func (c *Client) run(ctx context.Context, body map[string]any) ([]Doc, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/"+c.Index+"/_search", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opensearch status %d", resp.StatusCode)
	}
	var parsed struct {
		Hits struct {
			Hits []struct {
				Source Doc `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	docs := make([]Doc, 0, len(parsed.Hits.Hits))
	for _, h := range parsed.Hits.Hits {
		docs = append(docs, h.Source)
	}
	return docs, nil
}
