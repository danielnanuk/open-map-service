package route

import (
	"math"
	"testing"
)

// Google 官方文档参考例:precision 1e5
func TestDecodeGoogleReference(t *testing.T) {
	got := decodePolyline("_p~iF~ps|U_ulLnnqC_mqNvxq`@", 1e5)
	want := [][2]float64{{38.5, -120.2}, {40.7, -120.95}, {43.252, -126.453}}
	if len(got) != 3 {
		t.Fatalf("len=%d", len(got))
	}
	for i := range want {
		if math.Abs(got[i][0]-want[i][0]) > 1e-5 || math.Abs(got[i][1]-want[i][1]) > 1e-5 {
			t.Fatalf("point %d: got %v want %v", i, got[i], want[i])
		}
	}
}

func TestPolyline6To5RoundTrip(t *testing.T) {
	coords := [][2]float64{{11.5564, 104.9282}, {11.5600, 104.9300}, {13.3633, 103.8564}}
	out := Polyline6To5(EncodePolyline6(coords))
	got := decodePolyline(out, 1e5)
	if len(got) != len(coords) {
		t.Fatalf("len=%d", len(got))
	}
	for i := range coords {
		if math.Abs(got[i][0]-coords[i][0]) > 2e-5 || math.Abs(got[i][1]-coords[i][1]) > 2e-5 {
			t.Fatalf("point %d: got %v want %v", i, got[i], coords[i])
		}
	}
}

func TestPolylineEmptyAndNegative(t *testing.T) {
	if Polyline6To5("") != "" {
		t.Fatal("empty in, empty out")
	}
	got := decodePolyline(Polyline6To5(EncodePolyline6([][2]float64{{-38.5, -120.2}})), 1e5)
	if math.Abs(got[0][0]-(-38.5)) > 2e-5 || math.Abs(got[0][1]-(-120.2)) > 2e-5 {
		t.Fatalf("negative coords: %v", got)
	}
}

func TestDecodeValueTruncated(t *testing.T) {
	// 截断输入不得 panic:返回部分/空结果即可
	_ = Polyline6To5("_p~i")      // 截断在首值中段
	_ = Polyline6To5("_p~iF")     // lat 完整,lon 截断
	_ = Polyline6To5("_p~iF~ps|") // 两值完整,第三值截断
}
