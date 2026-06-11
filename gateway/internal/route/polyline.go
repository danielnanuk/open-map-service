// polyline 编解码:Valhalla 输出 precision 1e-6,Google 协议要求 1e-5,需转码。
package route

import (
	"math"
	"strings"
)

func decodeValue(encoded string, i int) (int64, int) {
	var result int64
	var shift uint
	for i < len(encoded) { // 截断输入不 panic:返回已解析部分
		b := int64(encoded[i]) - 63
		i++
		result |= (b & 0x1f) << shift
		shift += 5
		if b < 0x20 {
			break
		}
	}
	if result&1 == 1 {
		return ^(result >> 1), i
	}
	return result >> 1, i
}

func encodeValue(v int64, sb *strings.Builder) {
	u := v << 1
	if v < 0 {
		u = ^u
	}
	for u >= 0x20 {
		sb.WriteByte(byte(0x20|(u&0x1f)) + 63)
		u >>= 5
	}
	sb.WriteByte(byte(u) + 63)
}

func decodePolyline(encoded string, precision float64) [][2]float64 {
	var coords [][2]float64
	var lat, lon int64
	i := 0
	for i < len(encoded) {
		var dlat, dlon int64
		dlat, i = decodeValue(encoded, i)
		dlon, i = decodeValue(encoded, i)
		lat += dlat
		lon += dlon
		coords = append(coords, [2]float64{float64(lat) / precision, float64(lon) / precision})
	}
	return coords
}

// EncodePolyline6 按 precision 1e-6 编码坐标序列(测试与调试用)。
func EncodePolyline6(coords [][2]float64) string {
	var sb strings.Builder
	var prevLat, prevLon int64
	for _, c := range coords {
		lat := int64(math.Round(c[0] * 1e6))
		lon := int64(math.Round(c[1] * 1e6))
		encodeValue(lat-prevLat, &sb)
		encodeValue(lon-prevLon, &sb)
		prevLat, prevLon = lat, lon
	}
	return sb.String()
}

// Polyline6To5 把 Valhalla polyline6 转成 Google polyline5。
func Polyline6To5(encoded string) string {
	coords := decodePolyline(encoded, 1e6)
	var sb strings.Builder
	var prevLat, prevLon int64
	for _, c := range coords {
		lat := int64(math.Round(c[0] * 1e5))
		lon := int64(math.Round(c[1] * 1e5))
		encodeValue(lat-prevLat, &sb)
		encodeValue(lon-prevLon, &sb)
		prevLat, prevLon = lat, lon
	}
	return sb.String()
}
