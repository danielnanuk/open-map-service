// Valhalla sources_to_targets:小矩阵直连;超过位置上限(50)按 50×50 分块拼装。
package route

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const valhallaMatrixChunk = 50

type matrixCell struct {
	Time     *float64 `json:"time"`
	Distance *float64 `json:"distance"` // km(请求 units=kilometers)
}

func (c *Client) matrixCall(ctx context.Context, sources, targets []Location,
	costing string, costingOptions map[string]any) ([][]matrixCell, error) {
	body := map[string]any{
		"sources": sources, "targets": targets,
		"costing": costing, "units": "kilometers",
	}
	if costingOptions != nil {
		body["costing_options"] = costingOptions
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/sources_to_targets", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("valhalla matrix status %d", resp.StatusCode)
	}
	var parsed struct {
		Rows [][]matrixCell `json:"sources_to_targets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed.Rows) != len(sources) {
		return nil, fmt.Errorf("valhalla matrix: %d rows for %d sources", len(parsed.Rows), len(sources))
	}
	for i, row := range parsed.Rows {
		if len(row) != len(targets) {
			return nil, fmt.Errorf("valhalla matrix row %d: %d cols for %d targets", i, len(row), len(targets))
		}
	}
	return parsed.Rows, nil
}

// Matrix 返回 durations(秒)与 distances(米),nil=不可达;自动分块。
func (c *Client) Matrix(ctx context.Context, sources, targets []Location,
	costing string, costingOptions map[string]any) ([][]*float64, [][]*float64, error) {
	durs := make([][]*float64, len(sources))
	dists := make([][]*float64, len(sources))
	for i := range durs {
		durs[i] = make([]*float64, len(targets))
		dists[i] = make([]*float64, len(targets))
	}
	for si := 0; si < len(sources); si += valhallaMatrixChunk {
		sEnd := min(si+valhallaMatrixChunk, len(sources))
		for ti := 0; ti < len(targets); ti += valhallaMatrixChunk {
			tEnd := min(ti+valhallaMatrixChunk, len(targets))
			rows, err := c.matrixCall(ctx, sources[si:sEnd], targets[ti:tEnd], costing, costingOptions)
			if err != nil {
				return nil, nil, err
			}
			for i, row := range rows {
				for j, cell := range row {
					if cell.Time != nil && *cell.Time >= 0 {
						tv := *cell.Time
						durs[si+i][ti+j] = &tv
						if cell.Distance != nil {
							dv := *cell.Distance * 1000 // km → m
							dists[si+i][ti+j] = &dv
						}
					}
				}
			}
		}
	}
	return durs, dists, nil
}
