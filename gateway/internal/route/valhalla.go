// Valhalla /route 客户端。"找不到路径"等业务性 400(带 error_code)返回 (nil, nil)。
package route

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Location struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type LegSummary struct {
	Time   float64 `json:"time"`
	Length float64 `json:"length"` // 单位 km(请求 units=kilometers)
}

type Leg struct {
	Shape   string     `json:"shape"`
	Summary LegSummary `json:"summary"`
}

type Trip struct {
	Legs    []Leg      `json:"legs"`
	Summary LegSummary `json:"summary"`
	Status  int        `json:"status"`
	Units   string     `json:"units"`
}

type valhallaRequest struct {
	Locations      []Location     `json:"locations"`
	Costing        string         `json:"costing"`
	CostingOptions map[string]any `json:"costing_options,omitempty"`
	Units          string         `json:"units"`
	Language       string         `json:"language,omitempty"`
}

type valhallaResponse struct {
	Trip      Trip   `json:"trip"`
	ErrorCode int    `json:"error_code"`
	Error     string `json:"error"`
}

type Client struct {
	baseURL string
	http    *http.Client
}

func New(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) Route(ctx context.Context, locs []Location, costing string,
	costingOptions map[string]any, lang string) (*Trip, error) {
	payload, err := json.Marshal(valhallaRequest{
		Locations: locs, Costing: costing, CostingOptions: costingOptions,
		Units: "kilometers", Language: lang,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/route", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var parsed valhallaResponse
	if resp.StatusCode == http.StatusBadRequest {
		if json.NewDecoder(resp.Body).Decode(&parsed) == nil && parsed.ErrorCode != 0 {
			// 442 No path / 171 No suitable edges → 业务性无果;1xx(如 120 位置数不足)是调用方错误,
			// 但 handler 在调用前已校验两点,实践不可达 —— 统一按无果处理
			return nil, nil
		}
		return nil, fmt.Errorf("valhalla status 400")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("valhalla status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if parsed.Trip.Status != 0 {
		return nil, fmt.Errorf("valhalla partial route status %d", parsed.Trip.Status)
	}
	return &parsed.Trip, nil
}
