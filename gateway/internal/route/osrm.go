// OSRM /table 客户端:大矩阵专用。durations 秒、distances 米;null 单元格 = 不可达。
package route

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type OSRM struct {
	baseURL string
	http    *http.Client
}

func NewOSRM(baseURL string) *OSRM {
	return &OSRM{baseURL: baseURL, http: &http.Client{Timeout: 60 * time.Second}}
}

// Table 返回 durations(秒)与 distances(米),外层 len=sources、内层 len=targets;nil=不可达。
func (o *OSRM) Table(ctx context.Context, sources, targets []Location) ([][]*float64, [][]*float64, error) {
	var sb strings.Builder
	writeLoc := func(l Location) {
		sb.WriteString(strconv.FormatFloat(l.Lon, 'f', 6, 64))
		sb.WriteByte(',')
		sb.WriteString(strconv.FormatFloat(l.Lat, 'f', 6, 64))
	}
	for i, l := range sources {
		if i > 0 {
			sb.WriteByte(';')
		}
		writeLoc(l)
	}
	for _, l := range targets {
		sb.WriteByte(';')
		writeLoc(l)
	}
	idxs := func(from, n int) string {
		parts := make([]string, n)
		for i := 0; i < n; i++ {
			parts[i] = strconv.Itoa(from + i)
		}
		return strings.Join(parts, ";")
	}
	url := fmt.Sprintf("%s/table/v1/driving/%s?sources=%s&destinations=%s&annotations=duration,distance",
		o.baseURL, sb.String(), idxs(0, len(sources)), idxs(len(sources), len(targets)))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := o.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	var parsed struct {
		Code      string       `json:"code"`
		Message   string       `json:"message"`
		Durations [][]*float64 `json:"durations"`
		Distances [][]*float64 `json:"distances"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, nil, fmt.Errorf("osrm decode: %w (http %d)", err, resp.StatusCode)
	}
	if parsed.Code != "Ok" {
		return nil, nil, fmt.Errorf("osrm %s: %s", parsed.Code, parsed.Message)
	}
	// 形状契约在客户端边界强制:畸形响应变成干净错误,而不是 handler 越界 panic
	for name, m := range map[string][][]*float64{"durations": parsed.Durations, "distances": parsed.Distances} {
		if len(m) != len(sources) {
			return nil, nil, fmt.Errorf("osrm %s: %d rows for %d sources", name, len(m), len(sources))
		}
		for i, row := range m {
			if len(row) != len(targets) {
				return nil, nil, fmt.Errorf("osrm %s row %d: %d cols for %d targets", name, i, len(row), len(targets))
			}
		}
	}
	return parsed.Durations, parsed.Distances, nil
}

// Matrix 实现网关 MatrixRouter 接口(costing 已烘焙进图,参数忽略)。
func (o *OSRM) Matrix(ctx context.Context, sources, targets []Location,
	_ string, _ map[string]any) ([][]*float64, [][]*float64, error) {
	return o.Table(ctx, sources, targets)
}
