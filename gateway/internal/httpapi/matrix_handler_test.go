package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielnanuk/open-map-service/gateway/internal/route"
)

type fakeMatrix struct {
	hits int
	err  error
}

func (f *fakeMatrix) Matrix(_ context.Context, s, t []route.Location, _ string, _ map[string]any) ([][]*float64, [][]*float64, error) {
	f.hits++
	if f.err != nil {
		return nil, nil, f.err
	}
	durs := make([][]*float64, len(s))
	dists := make([][]*float64, len(s))
	for i := range s {
		durs[i] = make([]*float64, len(t))
		dists[i] = make([]*float64, len(t))
		for j := range t {
			v := float64(i*100 + j)
			d := v * 10
			durs[i][j], dists[i][j] = &v, &d
		}
	}
	durs[0][0], dists[0][0] = nil, nil // (0,0) 不可达,验证 ROUTE_NOT_FOUND
	return durs, dists, nil
}

func matrixBody(nOrig, nDest int, mode, profile string) string {
	wp := func(lat, lon float64) string {
		return fmt.Sprintf(`{"waypoint":{"location":{"latLng":{"latitude":%f,"longitude":%f}}}}`, lat, lon)
	}
	var o, d []string
	for i := 0; i < nOrig; i++ {
		o = append(o, wp(11.5+float64(i)*0.001, 104.9))
	}
	for i := 0; i < nDest; i++ {
		d = append(d, wp(11.6+float64(i)*0.001, 104.9))
	}
	extra := ""
	if profile != "" {
		extra = fmt.Sprintf(`,"vehicleProfile":%q`, profile)
	}
	return fmt.Sprintf(`{"origins":[%s],"destinations":[%s],"travelMode":%q%s}`,
		strings.Join(o, ","), strings.Join(d, ","), mode, extra)
}

func matrixPOST(t *testing.T, h *Handlers, body string) (int, []any, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("POST", "/distanceMatrix/v2:computeRouteMatrix", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ComputeRouteMatrix(rec, req)
	var arr []any
	if json.Unmarshal(rec.Body.Bytes(), &arr) == nil {
		return rec.Code, arr, nil
	}
	var obj map[string]any
	json.Unmarshal(rec.Body.Bytes(), &obj)
	return rec.Code, nil, obj
}

func TestMatrixSmallGoesToValhalla(t *testing.T) {
	vh, osrm := &fakeMatrix{}, &fakeMatrix{}
	h := NewWithGeocoder(&fakeSearcher{}, nil, &fakeGeocoder{}).
		WithMatrix(map[string]MatrixRouter{"auto": osrm}, vh)
	code, arr, _ := matrixPOST(t, h, matrixBody(3, 4, "DRIVE", ""))
	if code != 200 || len(arr) != 12 {
		t.Fatalf("code=%d len=%d", code, len(arr))
	}
	if vh.hits != 1 || osrm.hits != 0 {
		t.Fatalf("routing: vh=%d osrm=%d", vh.hits, osrm.hits)
	}
	first := arr[0].(map[string]any)
	if first["condition"] != "ROUTE_NOT_FOUND" {
		t.Fatalf("not-found cell: %v", first)
	}
	second := arr[1].(map[string]any)
	if second["condition"] != "ROUTE_EXISTS" || second["duration"] != "1s" ||
		second["distanceMeters"].(float64) != 10 {
		t.Fatalf("cell: %v", second)
	}
}

func TestMatrixLargeGoesToOSRM(t *testing.T) {
	vh, osrm := &fakeMatrix{}, &fakeMatrix{}
	h := NewWithGeocoder(&fakeSearcher{}, nil, &fakeGeocoder{}).
		WithMatrix(map[string]MatrixRouter{"auto": osrm}, vh)
	code, arr, _ := matrixPOST(t, h, matrixBody(51, 51, "DRIVE", "")) // 2601 > 2500
	if code != 200 || len(arr) != 2601 {
		t.Fatalf("code=%d len=%d", code, len(arr))
	}
	if osrm.hits != 1 || vh.hits != 0 {
		t.Fatalf("routing: vh=%d osrm=%d", vh.hits, osrm.hits)
	}
}

func TestMatrixOSRMDownFallsBackToValhalla(t *testing.T) {
	vh := &fakeMatrix{}
	osrm := &fakeMatrix{err: errors.New("connection refused")}
	h := NewWithGeocoder(&fakeSearcher{}, nil, &fakeGeocoder{}).
		WithMatrix(map[string]MatrixRouter{"auto": osrm}, vh)
	code, arr, _ := matrixPOST(t, h, matrixBody(51, 51, "DRIVE", ""))
	if code != 200 || len(arr) != 2601 {
		t.Fatalf("fallback failed: code=%d len=%d", code, len(arr))
	}
	if osrm.hits != 1 || vh.hits != 1 {
		t.Fatalf("routing: vh=%d osrm=%d", vh.hits, osrm.hits)
	}
}

func TestMatrixTuktukUsesItsInstanceAndWalkUsesValhalla(t *testing.T) {
	vh, tuk := &fakeMatrix{}, &fakeMatrix{}
	h := NewWithGeocoder(&fakeSearcher{}, nil, &fakeGeocoder{}).
		WithMatrix(map[string]MatrixRouter{"tuktuk": tuk}, vh)
	matrixPOST(t, h, matrixBody(51, 51, "TWO_WHEELER", "tuktuk"))
	if tuk.hits != 1 {
		t.Fatalf("tuktuk instance not used: %d", tuk.hits)
	}
	matrixPOST(t, h, matrixBody(51, 51, "WALK", "")) // 无 WALK 实例 → Valhalla
	if vh.hits != 1 {
		t.Fatalf("walk should go valhalla: %d", vh.hits)
	}
}

func TestMatrixValidation(t *testing.T) {
	h := NewWithGeocoder(&fakeSearcher{}, nil, &fakeGeocoder{}).
		WithMatrix(map[string]MatrixRouter{}, &fakeMatrix{})
	code, _, obj := matrixPOST(t, h, `{"origins":[],"destinations":[]}`)
	if code != 400 {
		t.Fatalf("empty: %d %v", code, obj)
	}
	code, _, _ = matrixPOST(t, h, matrixBody(1, 1, "TRANSIT", ""))
	if code != 400 {
		t.Fatalf("bad mode: %d", code)
	}
	code, _, _ = matrixPOST(t, h, matrixBody(1001, 1, "DRIVE", ""))
	if code != 400 {
		t.Fatalf("oversize: %d", code)
	}
}

func TestMatrixNotConfigured(t *testing.T) {
	h := NewWithGeocoder(&fakeSearcher{}, nil, &fakeGeocoder{}) // 未 WithMatrix
	code, _, _ := matrixPOST(t, h, matrixBody(1, 1, "DRIVE", ""))
	if code != 500 {
		t.Fatalf("want clean 500, got %d", code)
	}
}

func TestMatrixFieldMaskDoesNotBreakArray(t *testing.T) {
	vh := &fakeMatrix{}
	h := NewWithGeocoder(&fakeSearcher{}, nil, &fakeGeocoder{}).
		WithMatrix(map[string]MatrixRouter{}, vh)
	req := httptest.NewRequest("POST", "/distanceMatrix/v2:computeRouteMatrix",
		strings.NewReader(matrixBody(2, 2, "DRIVE", "")))
	req.Header.Set("X-Goog-FieldMask", "originIndex")
	rec := httptest.NewRecorder()
	h.ComputeRouteMatrix(rec, req)
	var arr []any
	if err := json.Unmarshal(rec.Body.Bytes(), &arr); err != nil || len(arr) != 4 {
		t.Fatalf("array response broken under mask: %v %s", err, rec.Body.String())
	}
}
