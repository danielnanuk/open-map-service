package route

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func fakeMatrixServer(t *testing.T, hits *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sources_to_targets" {
			t.Errorf("path %s", r.URL.Path)
		}
		*hits++
		var req struct {
			Sources []Location `json:"sources"`
			Targets []Location `json:"targets"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		rows := make([][]map[string]any, len(req.Sources))
		for i := range req.Sources {
			rows[i] = make([]map[string]any, len(req.Targets))
			for j := range req.Targets {
				rows[i][j] = map[string]any{"time": float64(i*10 + j), "distance": float64(i*10+j) / 100}
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"sources_to_targets": rows})
	}))
}

func matLocs(n int, base float64) []Location {
	out := make([]Location, n)
	for i := range out {
		out[i] = Location{Lat: base + float64(i)*0.001, Lon: 104.9 + float64(i)*0.001}
	}
	return out
}

func TestValhallaMatrixSingleCall(t *testing.T) {
	hits := 0
	srv := fakeMatrixServer(t, &hits)
	defer srv.Close()
	c := New(srv.URL)
	durs, dists, err := c.Matrix(context.Background(), matLocs(3, 11.5), matLocs(4, 11.6), "auto", nil)
	if err != nil || hits != 1 {
		t.Fatalf("err=%v hits=%d", err, hits)
	}
	if len(durs) != 3 || len(durs[0]) != 4 {
		t.Fatalf("shape: %dx%d", len(durs), len(durs[0]))
	}
	if *durs[2][3] != 23 || *dists[2][3] != 230 { // km→米:0.23km=230m
		t.Fatalf("values: %v %v", *durs[2][3], *dists[2][3])
	}
}

func TestValhallaMatrixChunks(t *testing.T) {
	hits := 0
	srv := fakeMatrixServer(t, &hits)
	defer srv.Close()
	c := New(srv.URL)
	// 60×60 → 上限 50 → 2×2 = 4 次分块,拼装后形状完整
	durs, _, err := c.Matrix(context.Background(), matLocs(60, 11.5), matLocs(60, 11.6), "auto", nil)
	if err != nil || hits != 4 {
		t.Fatalf("err=%v hits=%d", err, hits)
	}
	if len(durs) != 60 || len(durs[59]) != 60 || durs[59][59] == nil {
		t.Fatalf("assembled shape broken")
	}
}

func TestValhallaMatrixUnreachableCell(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// time 缺失/null = 不可达
		w.Write([]byte(`{"sources_to_targets":[[{"time":null,"distance":null}]]}`))
	}))
	defer srv.Close()
	c := New(srv.URL)
	durs, _, err := c.Matrix(context.Background(), matLocs(1, 11.5), matLocs(1, 11.6), "auto", nil)
	if err != nil || durs[0][0] != nil {
		t.Fatalf("unreachable should be nil: %v %v", durs, err)
	}
}
