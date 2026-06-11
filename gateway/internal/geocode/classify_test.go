package geocode

import "testing"

func TestAddressLike(t *testing.T) {
	cases := []struct {
		q    string
		want bool
	}{
		{"Street 271, Phnom Penh", true}, // 门牌/街道
		{"#36 Mao Tse Toung Blvd", true}, // 门牌号 + 大道
		{"ផ្លូវ ២៧១", true},              // 高棉文"街"
		{"ខេត្តសៀមរាប", true},            // 高棉文"省"
		{"Siem Reap province", true},     // 行政区
		{"សង្កាត់បឹងកេងកង", true},        // 高棉文"分区"
		{"Brown Coffee", false},          // 品牌 POI
		{"Royal Palace", false},          // 地标 POI
		{"អង្គរវត្ត", false},             // 高棉文 POI
		{"271", false},                   // 裸数字太短
	}
	for _, c := range cases {
		if got := AddressLike(c.q); got != c.want {
			t.Errorf("AddressLike(%q) = %v, want %v", c.q, got, c.want)
		}
	}
}
