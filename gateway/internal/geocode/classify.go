// AddressLike 判断查询更像"地址/行政区"(走 Nominatim)还是"POI 名称"(走 OpenSearch)。
// 规则刻意保守:误判只影响首选路径——零结果会回退到另一个后端,不影响可达性。
package geocode

import (
	"regexp"
	"strings"
)

var streetWords = []string{
	"street", " st ", " st.", "road", " rd", "avenue", " ave", "blvd", "boulevard",
	"highway", "national road", "ផ្លូវ", "វិថី", "មហាវិថី",
}

var adminWords = []string{
	"province", "district", "commune", "village",
	"ខេត្ត", "ស្រុក", "ខណ្ឌ", "ឃុំ", "សង្កាត់", "ភូមិ",
}

// 门牌样式:独立的数字串(可带 # 前缀/字母后缀),如 "271"、"#36"、"5b"
var houseNumberRe = regexp.MustCompile(`(^|[\s,])#?\d+[a-zA-Z]?($|[\s,])`)

func AddressLike(q string) bool {
	if len([]rune(q)) < 4 {
		return false
	}
	lq := " " + strings.ToLower(q) + " "
	for _, w := range adminWords {
		if strings.Contains(lq, w) {
			return true
		}
	}
	for _, w := range streetWords {
		if strings.Contains(lq, w) {
			return true
		}
	}
	return houseNumberRe.MatchString(q) && len([]rune(q)) >= 8
}
