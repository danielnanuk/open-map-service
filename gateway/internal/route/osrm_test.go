package route

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const tableJSON = `{"code":"Ok",
  "durations":[[0,100.5],[99.1,0]],
  "distances":[[0,1200.4],[1180.2,0]]}`

func TestOSRMTableParses(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		w.Write([]byte(tableJSON))
	}))
	defer srv.Close()
	c := NewOSRM(srv.URL)
	durs, dists, err := c.Table(context.Background(),
		[]Location{{Lat: 11.5564, Lon: 104.9282}, {Lat: 11.5696, Lon: 104.9210}},
		[]Location{{Lat: 11.5564, Lon: 104.9282}, {Lat: 11.5696, Lon: 104.9210}})
	if err != nil {
		t.Fatal(err)
	}
	if durs[0][1] == nil || *durs[0][1] != 100.5 || *dists[1][0] != 1180.2 {
		t.Fatalf("parse: %v %v", durs, dists)
	}
	if !strings.Contains(gotPath, "/table/v1/driving/104.928200,11.556400;104.921000,11.569600;104.928200,11.556400;104.921000,11.569600") ||
		!strings.Contains(gotPath, "sources=0;1") || !strings.Contains(gotPath, "destinations=2;3") ||
		!strings.Contains(gotPath, "annotations=duration,distance") {
		t.Fatalf("path: %s", gotPath)
	}
}

func TestOSRMTableNullCell(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":"Ok","durations":[[null]],"distances":[[null]]}`))
	}))
	defer srv.Close()
	c := NewOSRM(srv.URL)
	durs, _, err := c.Table(context.Background(),
		[]Location{{Lat: 1, Lon: 2}}, []Location{{Lat: 3, Lon: 4}})
	if err != nil || durs[0][0] != nil {
		t.Fatalf("null cell should stay nil: %v %v", durs, err)
	}
}

func TestOSRMTableErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"code":"TooBig","message":"Too many table coordinates"}`))
	}))
	defer srv.Close()
	c := NewOSRM(srv.URL)
	if _, _, err := c.Table(context.Background(),
		[]Location{{Lat: 1, Lon: 2}}, []Location{{Lat: 3, Lon: 4}}); err == nil {
		t.Fatal("want error")
	}
}

func TestOSRMMatrixAdapter(t *testing.T) { // MatrixRouter 适配:忽略 costing/options
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(tableJSON))
	}))
	defer srv.Close()
	c := NewOSRM(srv.URL)
	durs, _, err := c.Matrix(context.Background(),
		[]Location{{Lat: 1, Lon: 2}, {Lat: 3, Lon: 4}},
		[]Location{{Lat: 1, Lon: 2}, {Lat: 3, Lon: 4}}, "auto", map[string]any{"x": 1})
	if err != nil || *durs[0][1] != 100.5 {
		t.Fatalf("%v %v", durs, err)
	}
}
