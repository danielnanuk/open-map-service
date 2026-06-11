# M2:Nominatim 正/逆地理编码 + 网关分流 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 接入 Nominatim(柬埔寨库),在网关暴露 Google Geocoding API 形态的 `GET /maps/api/geocode/json`(正向地址→坐标、逆向坐标→地址+最近 POI),地址类查询走 Nominatim、POI 类查询走 OpenSearch,互为零结果回退。

**Architecture:** mediagis/nominatim 5.x 容器从已有的柬埔寨 PBF 一次性导入(~10-20GB 库);网关新增 `internal/geocode` 包(Nominatim jsonv2 客户端 + 地址判别器 + 双向结果翻译),`internal/store` 增加 KNN 最近 POI 查询;响应严格对齐 Google **legacy Geocoding** 形态(注意:坐标键是 `lat`/`lng`,与 Places New 的 `latitude`/`longitude` 不同;HTTP 恒 200,错误用顶层 `status` 表达)。

**Tech Stack:** mediagis/nominatim:5.1 · Go 1.25(复用 M1 gateway)· PostGIS KNN(`<->` + places_geog_idx)

**Spec:** `docs/superpowers/specs/2026-06-10-places-api-design.md` §6/§8/§13-M2。验收:地址/行政区查询样本通过(golden geocode 用例)。

**执行纪律(M1 经验,全部任务适用):**
- 所有命令前台执行 + 充足 timeout;唯一例外是 Nominatim 导入(容器内后台进行,用快速轮询观察)
- psql 一律 `docker exec places-postgis-1 psql -U places -d places ...`;审查/调试查询先 `SET statement_timeout='15s'`;严禁 places×osm_pois 的无索引 jsonb 交叉连接
- `make test-py-integration` 对共享库破坏性(TRUNCATE + alias 切换),跑过之后必须 `make etl-load etl-conflate etl-index` 恢复再做 e2e
- 新发布端口一律绑定 127.0.0.1(除 gateway 8080)
- M2 代码基于 M1 分支(`feat/m1-data-pipeline-search`)之上或合并后的 main

---

## 文件结构(全貌)

```
docker-compose.yml                          # 修改:加 nominatim 服务;up 目标加 nominatim
gateway/internal/gapi/geocoding.go          # 新:legacy Geocoding 响应类型(lat/lng!)
gateway/internal/geocode/nominatim.go       # 新:Nominatim jsonv2 客户端(Search/Reverse)
gateway/internal/geocode/nominatim_test.go
gateway/internal/geocode/classify.go        # 新:地址似然判别(km/en 关键词 + 门牌模式)
gateway/internal/geocode/classify_test.go
gateway/internal/geocode/translate.go       # 新:Nominatim/OS Doc → GeocodeResult
gateway/internal/geocode/translate_test.go
gateway/internal/store/store.go             # 修改:加 GetNearbyPlaces(KNN)
gateway/internal/httpapi/geocode_handler.go # 新:GET /maps/api/geocode/json(正/逆 + 分流)
gateway/internal/httpapi/geocode_handler_test.go
gateway/cmd/gateway/main.go                 # 修改:路由 + NOMINATIM_URL env
golden/cases.yaml                           # 修改:加 geocode/reverse 用例
golden/run_golden.py                        # 修改:支持 GET 端点
README.md                                   # 修改:M2 quickstart + 已知差异追加
Makefile                                    # 修改:up 目标加 nominatim
```

接口边界:`httpapi` 只依赖三个窄接口——`Searcher`(已有)、`Geocoder`(新,Search/Reverse)、`NearbyStore`(新,GetNearbyPlaces)——全部可用 fake 单测。

---

### Task 1:Nominatim 容器接入与导入

**Files:**
- Modify: `docker-compose.yml`, `Makefile`(`up` 目标)

- [ ] **Step 1: docker-compose.yml 追加服务**(注意:首次启动会执行一次性导入,柬埔寨约 15-60 分钟;数据卷在,重启不重导)

```yaml
  nominatim:
    image: mediagis/nominatim:5.1
    environment:
      PBF_PATH: /nominatim/data/cambodia-latest.osm.pbf
      NOMINATIM_PASSWORD: nominatim_places
      THREADS: "4"
    ports: ["127.0.0.1:8081:8080"]
    volumes:
      - ./data:/nominatim/data:ro
      - nominatim-data:/var/lib/postgresql/16/main
    healthcheck:
      test: ["CMD-SHELL", "curl -sf http://localhost:8080/status || exit 1"]
      interval: 30s
      timeout: 5s
      retries: 120
      start_period: 300s
```

并在顶层 `volumes:` 加 `nominatim-data:`。

注意:若 5.1 镜像的 PG 数据目录不是 `/var/lib/postgresql/16/main`(用 `docker image inspect mediagis/nominatim:5.1` 查 Volumes 段确认),改成镜像实际声明的路径并在报告中说明。

- [ ] **Step 2: Makefile 的 `up` 目标加 nominatim**

```makefile
up:
	docker compose up -d --build postgis opensearch nominatim
	docker compose ps
```

- [ ] **Step 3: 启动并轮询导入完成**

Run: `docker compose up -d nominatim`(立即返回,导入在容器内进行)
然后每 2-3 分钟前台快查一次(每次都是秒级命令,重复直到就绪,预算 60 分钟):

```bash
docker compose ps nominatim                       # 看 health 状态
docker logs places-nominatim-1 --tail 5          # 看导入进度
curl -sf http://localhost:8081/status && echo OK  # 就绪时输出 OK
```

- [ ] **Step 4: 验收冒烟**(包含在报告里)

```bash
curl -s 'http://localhost:8081/search?q=Royal+Palace+Phnom+Penh&format=jsonv2&limit=2' | head -c 800
curl -s 'http://localhost:8081/search?q=%E1%9E%81%E1%9F%81%E1%9E%8F%E1%9F%92%E1%9E%8F%E1%9E%9F%E1%9F%80%E1%9E%98%E1%9E%9A%E1%9E%B6%E1%9E%94&format=jsonv2&limit=2' | head -c 800
curl -s 'http://localhost:8081/reverse?lat=11.5621&lon=104.9160&format=jsonv2' | head -c 800
```

Expected: 三条都返回含 `display_name` 的 JSON(第二条是高棉文"ខេត្តសៀមរាប"/暹粒省)。

- [ ] **Step 5: Commit**

```bash
git add docker-compose.yml Makefile
git commit -m "feat: nominatim 5.1 service with cambodia import"
```

---

### Task 2:legacy Geocoding 类型 + Nominatim 客户端(TDD)

**Files:**
- Create: `gateway/internal/gapi/geocoding.go`, `gateway/internal/geocode/nominatim.go`, `gateway/internal/geocode/nominatim_test.go`

- [ ] **Step 1: 类型定义**(legacy 形态,键名是协议本身,逐字核对)

```go
// gateway/internal/gapi/geocoding.go
package gapi

// legacy Geocoding API 的响应形态:HTTP 恒 200,错误用顶层 status 表达。
// 注意坐标键是 lat/lng(legacy),不是 Places New 的 latitude/longitude。
type GeoLatLng struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type AddressComponent struct {
	LongName  string   `json:"long_name"`
	ShortName string   `json:"short_name"`
	Types     []string `json:"types"`
}

type GeocodeGeometry struct {
	Location     GeoLatLng `json:"location"`
	LocationType string    `json:"location_type"` // ROOFTOP | GEOMETRIC_CENTER | APPROXIMATE
}

type GeocodeResult struct {
	AddressComponents []AddressComponent `json:"address_components"`
	FormattedAddress  string             `json:"formatted_address"`
	Geometry          GeocodeGeometry    `json:"geometry"`
	PlaceID           string             `json:"place_id"`
	Types             []string           `json:"types"`
}

type GeocodeResponse struct {
	Results []GeocodeResult `json:"results"`
	Status  string          `json:"status"` // OK | ZERO_RESULTS | INVALID_REQUEST | UNKNOWN_ERROR
}
```

- [ ] **Step 2: 写失败的客户端测试**(httptest 假 Nominatim)

```go
// gateway/internal/geocode/nominatim_test.go
package geocode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const searchJSON = `[{"place_id":123,"osm_type":"way","osm_id":456,
  "lat":"11.5563","lon":"104.9282","name":"Royal Palace",
  "display_name":"Royal Palace, Phnom Penh, Cambodia","addresstype":"tourism",
  "address":{"tourism":"Royal Palace","road":"Sothearos Blvd","city":"Phnom Penh",
             "country":"Cambodia","country_code":"kh","postcode":"12301"}}]`

func fakeNominatim(t *testing.T, wantPath string, body string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wantPath {
			t.Errorf("path = %s, want %s", r.URL.Path, wantPath)
		}
		if r.URL.Query().Get("format") != "jsonv2" {
			t.Errorf("format param missing: %s", r.URL.RawQuery)
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
}

func TestSearchParsesResults(t *testing.T) {
	srv := fakeNominatim(t, "/search", searchJSON, 200)
	defer srv.Close()
	c := NewNominatim(srv.URL)
	res, err := c.Search(context.Background(), "royal palace", "en", 5)
	if err != nil || len(res) != 1 {
		t.Fatalf("err=%v res=%v", err, res)
	}
	r := res[0]
	if r.OsmType != "way" || r.OsmID != 456 || r.DisplayName == "" {
		t.Fatalf("parse: %+v", r)
	}
	lat, lon, err := r.Coords()
	if err != nil || lat != 11.5563 || lon != 104.9282 {
		t.Fatalf("coords: %v %v %v", lat, lon, err)
	}
	if r.Address["city"] != "Phnom Penh" {
		t.Fatalf("address: %v", r.Address)
	}
}

func TestReverseNotFoundIsNilNil(t *testing.T) {
	srv := fakeNominatim(t, "/reverse", `{"error":"Unable to geocode"}`, 200)
	defer srv.Close()
	c := NewNominatim(srv.URL)
	r, err := c.Reverse(context.Background(), 0.1, 0.1, "en")
	if err != nil || r != nil {
		t.Fatalf("want nil,nil got %v,%v", r, err)
	}
}

func TestSearchHTTPErrorSurfaces(t *testing.T) {
	srv := fakeNominatim(t, "/search", "boom", 500)
	defer srv.Close()
	c := NewNominatim(srv.URL)
	if _, err := c.Search(context.Background(), "x", "en", 5); err == nil {
		t.Fatal("want error on 500")
	}
}
```

- [ ] **Step 3: 跑测试确认失败**

Run: `cd gateway && go test ./internal/geocode/`
Expected: FAIL,`undefined: NewNominatim`

- [ ] **Step 4: 实现客户端**

```go
// gateway/internal/geocode/nominatim.go
// Nominatim jsonv2 客户端:Search 正向、Reverse 逆向。Reverse 查无结果返回 (nil, nil)。
package geocode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type NominatimResult struct {
	OsmType     string            `json:"osm_type"`
	OsmID       int64             `json:"osm_id"`
	Lat         string            `json:"lat"`
	Lon         string            `json:"lon"`
	Name        string            `json:"name"`
	DisplayName string            `json:"display_name"`
	Addresstype string            `json:"addresstype"`
	Address     map[string]string `json:"address"`
	Error       string            `json:"error"`
}

func (r NominatimResult) Coords() (lat, lon float64, err error) {
	lat, err = strconv.ParseFloat(r.Lat, 64)
	if err != nil {
		return 0, 0, err
	}
	lon, err = strconv.ParseFloat(r.Lon, 64)
	return lat, lon, err
}

type Nominatim struct {
	baseURL string
	http    *http.Client
}

func NewNominatim(baseURL string) *Nominatim {
	return &Nominatim{baseURL: baseURL, http: &http.Client{Timeout: 10 * time.Second}}
}

func (n *Nominatim) get(ctx context.Context, path string, params url.Values, out any) error {
	params.Set("format", "jsonv2")
	params.Set("addressdetails", "1")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		n.baseURL+path+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := n.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("nominatim status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (n *Nominatim) Search(ctx context.Context, q, lang string, limit int) ([]NominatimResult, error) {
	params := url.Values{"q": {q}, "limit": {strconv.Itoa(limit)}, "countrycodes": {"kh"}}
	if lang != "" {
		params.Set("accept-language", lang)
	}
	var out []NominatimResult
	if err := n.get(ctx, "/search", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (n *Nominatim) Reverse(ctx context.Context, lat, lon float64, lang string) (*NominatimResult, error) {
	params := url.Values{
		"lat": {strconv.FormatFloat(lat, 'f', -1, 64)},
		"lon": {strconv.FormatFloat(lon, 'f', -1, 64)},
	}
	if lang != "" {
		params.Set("accept-language", lang)
	}
	var out NominatimResult
	if err := n.get(ctx, "/reverse", params, &out); err != nil {
		return nil, err
	}
	if out.Error != "" { // {"error":"Unable to geocode"}
		return nil, nil
	}
	return &out, nil
}
```

- [ ] **Step 5: 跑测试确认通过**(3 个测试)+ gofmt/vet 干净

- [ ] **Step 6: Commit**

```bash
git add gateway/
git commit -m "feat: legacy geocoding types and nominatim jsonv2 client"
```

---

### Task 3:地址似然判别器(TDD)

**Files:**
- Create: `gateway/internal/geocode/classify.go`, `gateway/internal/geocode/classify_test.go`

- [ ] **Step 1: 写失败测试**

```go
// gateway/internal/geocode/classify_test.go
package geocode

import "testing"

func TestAddressLike(t *testing.T) {
	cases := []struct {
		q    string
		want bool
	}{
		{"Street 271, Phnom Penh", true},     // 门牌/街道
		{"#36 Mao Tse Toung Blvd", true},     // 门牌号 + 大道
		{"ផ្លូវ ២៧១", true},                   // 高棉文"街"
		{"ខេត្តសៀមរាប", true},                 // 高棉文"省"
		{"Siem Reap province", true},          // 行政区
		{"សង្កាត់បឹងកេងកង", true},             // 高棉文"分区"
		{"Brown Coffee", false},               // 品牌 POI
		{"Royal Palace", false},               // 地标 POI
		{"អង្គរវត្ត", false},                  // 高棉文 POI
		{"271", false},                        // 裸数字太短
	}
	for _, c := range cases {
		if got := AddressLike(c.q); got != c.want {
			t.Errorf("AddressLike(%q) = %v, want %v", c.q, got, c.want)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**(`undefined: AddressLike`)

- [ ] **Step 3: 实现**

```go
// gateway/internal/geocode/classify.go
// AddressLike 判断查询更像"地址/行政区"(走 Nominatim)还是"POI 名称"(走 OpenSearch)。
// 规则刻意保守:误判为 POI 的地址会经零结果回退仍到达 Nominatim(反之亦然),
// 判别只影响首选路径,不影响可达性。
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
```

- [ ] **Step 4: 跑测试确认通过** + gofmt/vet

- [ ] **Step 5: Commit**

```bash
git add gateway/
git commit -m "feat: address-likeness classifier for geocode routing"
```

---

### Task 4:结果翻译层(TDD)

**Files:**
- Create: `gateway/internal/geocode/translate.go`, `gateway/internal/geocode/translate_test.go`

- [ ] **Step 1: 写失败测试**

```go
// gateway/internal/geocode/translate_test.go
package geocode

import (
	"testing"

	"github.com/danielnanuk/open-map-service/gateway/internal/search"
)

func TestNominatimToGeocodeResult(t *testing.T) {
	n := NominatimResult{
		OsmType: "way", OsmID: 456, Lat: "11.5563", Lon: "104.9282",
		DisplayName: "271, Street 271, Phnom Penh, 12311, Cambodia",
		Addresstype: "road",
		Address: map[string]string{
			"house_number": "271", "road": "Street 271", "city": "Phnom Penh",
			"postcode": "12311", "country": "Cambodia", "country_code": "kh",
		},
	}
	g, err := NominatimToGeocodeResult(n)
	if err != nil {
		t.Fatal(err)
	}
	if g.PlaceID != "nominatim:way:456" {
		t.Fatalf("place_id: %s", g.PlaceID)
	}
	if g.Geometry.Location.Lat != 11.5563 || g.Geometry.Location.Lng != 104.9282 {
		t.Fatalf("location: %+v", g.Geometry.Location)
	}
	if g.Geometry.LocationType != "ROOFTOP" { // 有 house_number
		t.Fatalf("location_type: %s", g.Geometry.LocationType)
	}
	comp := map[string]string{}
	for _, c := range g.AddressComponents {
		comp[c.Types[0]] = c.LongName
	}
	if comp["street_number"] != "271" || comp["route"] != "Street 271" ||
		comp["locality"] != "Phnom Penh" || comp["postal_code"] != "12311" {
		t.Fatalf("components: %v", comp)
	}
	// country 的 short_name 取 country_code 大写
	for _, c := range g.AddressComponents {
		if c.Types[0] == "country" && c.ShortName != "KH" {
			t.Fatalf("country short: %v", c)
		}
	}
}

func TestNominatimLocationTypeFallbacks(t *testing.T) {
	road := NominatimResult{Lat: "1", Lon: "2", Address: map[string]string{"road": "X"}}
	g, _ := NominatimToGeocodeResult(road)
	if g.Geometry.LocationType != "GEOMETRIC_CENTER" {
		t.Fatalf("road-level: %s", g.Geometry.LocationType)
	}
	admin := NominatimResult{Lat: "1", Lon: "2", Address: map[string]string{"state": "Siem Reap"}}
	g, _ = NominatimToGeocodeResult(admin)
	if g.Geometry.LocationType != "APPROXIMATE" {
		t.Fatalf("admin-level: %s", g.Geometry.LocationType)
	}
}

func TestDocToGeocodeResult(t *testing.T) {
	d := search.Doc{PlaceID: "p1", NameDefault: "Angkor Wat",
		FormattedAddress: "Siem Reap, Cambodia", Categories: []string{"tourist_attraction"}}
	d.Location.Lat, d.Location.Lon = 13.4125, 103.867
	g := DocToGeocodeResult(d)
	if g.PlaceID != "p1" || g.Geometry.Location.Lat != 13.4125 {
		t.Fatalf("%+v", g)
	}
	if g.FormattedAddress != "Angkor Wat, Siem Reap, Cambodia" {
		t.Fatalf("formatted: %s", g.FormattedAddress)
	}
	if g.Geometry.LocationType != "GEOMETRIC_CENTER" || g.Types[0] != "tourist_attraction" {
		t.Fatalf("%+v", g)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**(`undefined: NominatimToGeocodeResult`)

- [ ] **Step 3: 实现**

```go
// gateway/internal/geocode/translate.go
// 把 Nominatim 结果 / OpenSearch Doc 翻译成 legacy Geocoding 形态。
package geocode

import (
	"strings"

	"github.com/danielnanuk/open-map-service/gateway/internal/gapi"
	"github.com/danielnanuk/open-map-service/gateway/internal/search"
)

// Nominatim address 键 → Google address_components 类型
var componentTypes = []struct {
	key    string
	gtypes []string
}{
	{"house_number", []string{"street_number"}},
	{"road", []string{"route"}},
	{"neighbourhood", []string{"sublocality", "political"}},
	{"quarter", []string{"sublocality", "political"}},
	{"suburb", []string{"sublocality", "political"}},
	{"village", []string{"locality", "political"}},
	{"town", []string{"locality", "political"}},
	{"city", []string{"locality", "political"}},
	{"county", []string{"administrative_area_level_2", "political"}},
	{"state", []string{"administrative_area_level_1", "political"}},
	{"postcode", []string{"postal_code"}},
	{"country", []string{"country", "political"}},
}

func NominatimToGeocodeResult(n NominatimResult) (gapi.GeocodeResult, error) {
	lat, lon, err := n.Coords()
	if err != nil {
		return gapi.GeocodeResult{}, err
	}
	comps := []gapi.AddressComponent{}
	for _, ct := range componentTypes {
		v := n.Address[ct.key]
		if v == "" {
			continue
		}
		short := v
		if ct.key == "country" {
			short = strings.ToUpper(n.Address["country_code"])
		}
		comps = append(comps, gapi.AddressComponent{LongName: v, ShortName: short, Types: ct.gtypes})
	}
	locType := "APPROXIMATE"
	rtypes := []string{"geocode"}
	switch {
	case n.Address["house_number"] != "":
		locType, rtypes = "ROOFTOP", []string{"street_address"}
	case n.Address["road"] != "":
		locType, rtypes = "GEOMETRIC_CENTER", []string{"route"}
	case n.Address["city"] != "" || n.Address["town"] != "" || n.Address["village"] != "":
		rtypes = []string{"locality", "political"}
	case n.Address["state"] != "":
		rtypes = []string{"administrative_area_level_1", "political"}
	}
	return gapi.GeocodeResult{
		AddressComponents: comps,
		FormattedAddress:  n.DisplayName,
		Geometry: gapi.GeocodeGeometry{
			Location:     gapi.GeoLatLng{Lat: lat, Lng: lon},
			LocationType: locType,
		},
		PlaceID: "nominatim:" + n.OsmType + ":" + itoa(n.OsmID),
		Types:   rtypes,
	}, nil
}

func itoa(v int64) string {
	return strings.TrimSpace(strconvFormatInt(v))
}
```

注意:`strconvFormatInt` 不存在——直接 `import "strconv"`,`PlaceID` 行写成:

```go
		PlaceID: "nominatim:" + n.OsmType + ":" + strconv.FormatInt(n.OsmID, 10),
```

并删掉 `itoa` 辅助函数。完整收尾:

```go
func DocToGeocodeResult(d search.Doc) gapi.GeocodeResult {
	return gapi.GeocodeResult{
		AddressComponents: []gapi.AddressComponent{},
		FormattedAddress:  d.NameDefault + ", " + d.FormattedAddress,
		Geometry: gapi.GeocodeGeometry{
			Location:     gapi.GeoLatLng{Lat: d.Location.Lat, Lng: d.Location.Lon},
			LocationType: "GEOMETRIC_CENTER",
		},
		PlaceID: d.PlaceID,
		Types:   d.Categories,
	}
}
```

- [ ] **Step 4: 跑测试确认通过** + gofmt/vet

- [ ] **Step 5: Commit**

```bash
git add gateway/
git commit -m "feat: nominatim/opensearch to legacy geocoding translation"
```

---

### Task 5:store.GetNearbyPlaces(KNN)

**Files:**
- Modify: `gateway/internal/store/store.go`

- [ ] **Step 1: 实现**(PostGIS KNN;`places_geog_idx` 已存在,ST_DWithin 走索引)

```go
// 追加到 gateway/internal/store/store.go
// GetNearbyPlaces 返回距 (lat,lon) 半径 radiusM 内最近的 limit 个地点(近→远)。
func (p *PG) GetNearbyPlaces(ctx context.Context, lat, lon, radiusM float64, limit int) ([]PlaceRow, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT place_id, names, categories,
		       COALESCE(phone,''), COALESCE(website,''), COALESCE(opening_hours,''),
		       COALESCE(address, '{}'::jsonb), ST_X(geom), ST_Y(geom)
		FROM places
		WHERE ST_DWithin(geom::geography, ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography, $3)
		ORDER BY geom <-> ST_SetSRID(ST_MakePoint($2, $1), 4326)
		LIMIT $4`, lat, lon, radiusM, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlaceRow
	for rows.Next() {
		var r PlaceRow
		if err := rows.Scan(&r.PlaceID, &r.Names, &r.Categories,
			&r.Phone, &r.Website, &r.OpeningHours, &r.Address, &r.Lon, &r.Lat); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
```

(参数顺序注意:`ST_MakePoint(lon, lat)`——$2 是 lon、$1 是 lat,上面以命名参数位次写定,实现时保持一致。)

- [ ] **Step 2: 编译 + 既有测试不回归**

Run: `cd gateway && go test ./... -count=1` — PASS;gofmt/vet 干净。

- [ ] **Step 3: 真实数据冒烟**(places 表需有数据;若被集成测试清空先 `make etl-load etl-conflate`)

写临时 `go run`(不提交):`GetNearbyPlaces(ctx, 11.5621, 104.9160, 150, 5)`,打印 place_id + names.default + 距离感(顺序应近→远)。把输出放进报告,删除临时文件。

- [ ] **Step 4: Commit**

```bash
git add gateway/
git commit -m "feat: store KNN nearby places query"
```

---

### Task 6:正向 geocode handler(TDD)

**Files:**
- Create: `gateway/internal/httpapi/geocode_handler.go`, `gateway/internal/httpapi/geocode_handler_test.go`

- [ ] **Step 1: 写失败测试**(fake Geocoder/Searcher;覆盖:地址优先走 Nominatim、POI 优先走 OS、双向零结果回退、双后端失败 UNKNOWN_ERROR、缺参 INVALID_REQUEST——legacy 协议 HTTP 恒 200)

```go
// gateway/internal/httpapi/geocode_handler_test.go
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/danielnanuk/open-map-service/gateway/internal/geocode"
	"github.com/danielnanuk/open-map-service/gateway/internal/search"
)

type fakeGeocoder struct {
	searchRes  []geocode.NominatimResult
	reverseRes *geocode.NominatimResult
	err        error
	searchHits int
}

func (f *fakeGeocoder) Search(_ context.Context, q, lang string, limit int) ([]geocode.NominatimResult, error) {
	f.searchHits++
	return f.searchRes, f.err
}
func (f *fakeGeocoder) Reverse(_ context.Context, lat, lon float64, lang string) (*geocode.NominatimResult, error) {
	return f.reverseRes, f.err
}

func nmResult() geocode.NominatimResult {
	return geocode.NominatimResult{OsmType: "way", OsmID: 9, Lat: "11.5", Lon: "104.9",
		DisplayName: "Street 271, Phnom Penh, Cambodia",
		Address:     map[string]string{"road": "Street 271", "city": "Phnom Penh", "country": "Cambodia", "country_code": "kh"}}
}

func geocodeGET(t *testing.T, h *Handlers, query string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("GET", "/maps/api/geocode/json"+query, nil)
	rec := httptest.NewRecorder()
	h.Geocode(rec, req)
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestGeocodeAddressPrefersNominatim(t *testing.T) {
	g := &fakeGeocoder{searchRes: []geocode.NominatimResult{nmResult()}}
	h := NewWithGeocoder(&fakeSearcher{}, nil, g)
	code, body := geocodeGET(t, h, "?address=Street+271+Phnom+Penh")
	if code != 200 || body["status"] != "OK" {
		t.Fatalf("%d %v", code, body)
	}
	r0 := body["results"].([]any)[0].(map[string]any)
	if r0["place_id"] != "nominatim:way:9" {
		t.Fatalf("place_id: %v", r0)
	}
	loc := r0["geometry"].(map[string]any)["location"].(map[string]any)
	if loc["lat"] != 11.5 || loc["lng"] != 104.9 { // legacy 键名!
		t.Fatalf("location keys: %v", loc)
	}
}

func TestGeocodePOIPrefersOpenSearchAndSkipsNominatim(t *testing.T) {
	g := &fakeGeocoder{searchRes: []geocode.NominatimResult{nmResult()}}
	h := NewWithGeocoder(&fakeSearcher{docs: []search.Doc{doc()}}, nil, g)
	_, body := geocodeGET(t, h, "?address=Royal+Palace")
	r0 := body["results"].([]any)[0].(map[string]any)
	if r0["place_id"] != "p1" { // OS 结果在前
		t.Fatalf("want OS-first, got %v", r0)
	}
	if g.searchHits != 0 { // OS 有结果时不应触发 Nominatim
		t.Fatalf("nominatim should not be called, hits=%d", g.searchHits)
	}
}

func TestGeocodeFallbackOnZeroResults(t *testing.T) {
	g := &fakeGeocoder{searchRes: []geocode.NominatimResult{nmResult()}}
	h := NewWithGeocoder(&fakeSearcher{}, nil, g) // OS 零结果
	_, body := geocodeGET(t, h, "?address=Some+POI+Name")
	if body["status"] != "OK" { // 回退到 Nominatim
		t.Fatalf("fallback failed: %v", body)
	}
}

func TestGeocodeBothEmpty(t *testing.T) {
	h := NewWithGeocoder(&fakeSearcher{}, nil, &fakeGeocoder{})
	_, body := geocodeGET(t, h, "?address=zzzz+nothing")
	if body["status"] != "ZERO_RESULTS" {
		t.Fatalf("%v", body)
	}
}

func TestGeocodeBackendsDownIsUnknownError(t *testing.T) {
	g := &fakeGeocoder{err: errors.New("down")}
	h := NewWithGeocoder(&errSearcher{}, nil, g)
	code, body := geocodeGET(t, h, "?address=Street+271")
	if code != 200 || body["status"] != "UNKNOWN_ERROR" { // legacy:HTTP 200 + status
		t.Fatalf("%d %v", code, body)
	}
}

func TestGeocodeMissingParams(t *testing.T) {
	h := NewWithGeocoder(&fakeSearcher{}, nil, &fakeGeocoder{})
	code, body := geocodeGET(t, h, "")
	if code != 200 || body["status"] != "INVALID_REQUEST" {
		t.Fatalf("%d %v", code, body)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**(`undefined: NewWithGeocoder`)

- [ ] **Step 3: 实现**

```go
// gateway/internal/httpapi/geocode_handler.go
// GET /maps/api/geocode/json —— legacy Geocoding 协议:HTTP 恒 200,状态在 body.status。
package httpapi

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielnanuk/open-map-service/gateway/internal/gapi"
	"github.com/danielnanuk/open-map-service/gateway/internal/geocode"
	"github.com/danielnanuk/open-map-service/gateway/internal/search"
)

type Geocoder interface {
	Search(ctx context.Context, q, lang string, limit int) ([]geocode.NominatimResult, error)
	Reverse(ctx context.Context, lat, lon float64, lang string) (*geocode.NominatimResult, error)
}

// NewWithGeocoder 在 New 基础上挂接 Nominatim。
func NewWithGeocoder(s Searcher, ps PlaceStore, g Geocoder) *Handlers {
	h := New(s, ps)
	h.geocoder = g
	return h
}

const geocodeLimit = 5

func writeGeocode(w http.ResponseWriter, results []gapi.GeocodeResult, status string) {
	if results == nil {
		results = []gapi.GeocodeResult{}
	}
	if status == "" {
		status = "OK"
		if len(results) == 0 {
			status = "ZERO_RESULTS"
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	jsonEncode(w, gapi.GeocodeResponse{Results: results, Status: status})
}

func (h *Handlers) Geocode(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lang := q.Get("language")
	switch {
	case q.Get("latlng") != "":
		h.reverseGeocode(w, r, q.Get("latlng"), lang)
	case q.Get("address") != "":
		h.forwardGeocode(w, r, q.Get("address"), lang)
	default:
		writeGeocode(w, nil, "INVALID_REQUEST")
	}
}

func (h *Handlers) forwardGeocode(w http.ResponseWriter, r *http.Request, address, lang string) {
	ctx := r.Context()
	nominatimFirst := geocode.AddressLike(address)
	var results []gapi.GeocodeResult
	var errCount int

	tryNominatim := func() {
		res, err := h.geocoder.Search(ctx, address, lang, geocodeLimit)
		if err != nil {
			log.Printf("geocode nominatim: %v", err)
			errCount++
			return
		}
		for _, n := range res {
			if g, err := geocode.NominatimToGeocodeResult(n); err == nil {
				results = append(results, g)
			}
		}
	}
	tryOpenSearch := func() {
		docs, err := h.searcher.SearchText(ctx, address, nil, geocodeLimit)
		if err != nil {
			log.Printf("geocode opensearch: %v", err)
			errCount++
			return
		}
		for _, d := range docs {
			results = append(results, geocode.DocToGeocodeResult(d))
		}
	}

	if nominatimFirst {
		tryNominatim()
		if len(results) == 0 {
			tryOpenSearch()
		}
	} else {
		tryOpenSearch()
		if len(results) == 0 {
			tryNominatim()
		}
	}
	if len(results) == 0 && errCount == 2 {
		writeGeocode(w, nil, "UNKNOWN_ERROR")
		return
	}
	writeGeocode(w, results, "")
}
```

`jsonEncode` 不存在——在 `respond.go` 加一个小工具(或直接在 writeGeocode 里 `json.NewEncoder(w).Encode(...)`,二选一,推荐后者并删去 jsonEncode 引用):

```go
	json.NewEncoder(w).Encode(gapi.GeocodeResponse{Results: results, Status: status})
```
(import "encoding/json")

`Handlers` 结构体需加字段(在 handlers.go):

```go
type Handlers struct {
	searcher Searcher
	store    PlaceStore
	geocoder Geocoder
}
```

`reverseGeocode` 在 Task 7 实现——本任务先放一个编译占位:

```go
func (h *Handlers) reverseGeocode(w http.ResponseWriter, r *http.Request, latlng, lang string) {
	writeGeocode(w, nil, "INVALID_REQUEST")
}
```
(Task 7 会替换它并带测试;此占位行为对 latlng 请求返回 INVALID_REQUEST,不影响 Task 6 的测试。)

还需为 `errCount == 2` 的判断成立:当 nominatimFirst 时 OS 只在零结果时才尝试——若 Nominatim 报错、OS 正常但零结果,errCount=1,正确得 ZERO_RESULTS;两个都报错才 UNKNOWN_ERROR。`strconv`/`strings` 若未用到从 import 删去(gofmt/vet 把关)。

- [ ] **Step 4: 跑测试确认通过**(6 个新测试)+ 全包回归 + gofmt/vet

- [ ] **Step 5: Commit**

```bash
git add gateway/
git commit -m "feat: forward geocode endpoint with nominatim/opensearch routing"
```

---

### Task 7:逆向 geocode handler(TDD)

**Files:**
- Modify: `gateway/internal/httpapi/geocode_handler.go`, `gateway/internal/httpapi/geocode_handler_test.go`, `gateway/internal/httpapi/handlers.go`(PlaceStore 接口)

- [ ] **Step 1: 写失败测试**

```go
// 追加到 geocode_handler_test.go
type fakeNearbyStore struct {
	fakeStore
	nearby []store.PlaceRow
}

func (f *fakeNearbyStore) GetNearbyPlaces(_ context.Context, lat, lon, radiusM float64, limit int) ([]store.PlaceRow, error) {
	return f.nearby, nil
}

func TestReverseGeocodeCombinesAddressAndPOIs(t *testing.T) {
	nm := nmResult()
	g := &fakeGeocoder{reverseRes: &nm}
	st := &fakeNearbyStore{nearby: []store.PlaceRow{{
		PlaceID: "p9", Names: map[string]string{"default": "Brown Coffee"},
		Categories: []string{"cafe"}, Address: map[string]string{"locality": "Phnom Penh"},
		Lon: 104.916, Lat: 11.5621,
	}}}
	h := NewWithGeocoder(&fakeSearcher{}, st, g)
	_, body := geocodeGET(t, h, "?latlng=11.5621,104.9160")
	if body["status"] != "OK" {
		t.Fatalf("%v", body)
	}
	results := body["results"].([]any)
	if len(results) != 2 { // 地址结果 + 1 个 POI
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if results[0].(map[string]any)["place_id"] != "nominatim:way:9" {
		t.Fatalf("address first: %v", results[0])
	}
	if results[1].(map[string]any)["place_id"] != "p9" {
		t.Fatalf("poi second: %v", results[1])
	}
}

func TestReverseGeocodeBadLatlng(t *testing.T) {
	h := NewWithGeocoder(&fakeSearcher{}, &fakeNearbyStore{}, &fakeGeocoder{})
	_, body := geocodeGET(t, h, "?latlng=garbage")
	if body["status"] != "INVALID_REQUEST" {
		t.Fatalf("%v", body)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**(reverse 占位返回 INVALID_REQUEST → 第一个测试 FAIL)

- [ ] **Step 3: 实现**

`handlers.go` 的 `PlaceStore` 接口追加方法(fakeStore 在测试里已有 GetPlace;fakeNearbyStore 内嵌它补 GetNearbyPlaces;`store.PG` 已实现两者):

```go
type PlaceStore interface {
	GetPlace(ctx context.Context, id string) (*store.PlaceRow, error)
	GetNearbyPlaces(ctx context.Context, lat, lon, radiusM float64, limit int) ([]store.PlaceRow, error)
}
```

注意:既有测试里 `fakeStore` 只实现了 GetPlace——给它补一个空实现以保持全部测试编译:

```go
func (f *fakeStore) GetNearbyPlaces(context.Context, float64, float64, float64, int) ([]store.PlaceRow, error) {
	return nil, nil
}
```

替换 `reverseGeocode` 占位:

```go
const reversePOIRadiusM = 150
const reversePOILimit = 4

func (h *Handlers) reverseGeocode(w http.ResponseWriter, r *http.Request, latlng, lang string) {
	parts := strings.Split(latlng, ",")
	if len(parts) != 2 {
		writeGeocode(w, nil, "INVALID_REQUEST")
		return
	}
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lon, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil {
		writeGeocode(w, nil, "INVALID_REQUEST")
		return
	}
	ctx := r.Context()
	var results []gapi.GeocodeResult
	if n, err := h.geocoder.Reverse(ctx, lat, lon, lang); err != nil {
		log.Printf("reverse nominatim: %v", err)
	} else if n != nil {
		if g, err := geocode.NominatimToGeocodeResult(*n); err == nil {
			results = append(results, g)
		}
	}
	if rows, err := h.store.GetNearbyPlaces(ctx, lat, lon, reversePOIRadiusM, reversePOILimit); err != nil {
		log.Printf("reverse nearby: %v", err)
	} else {
		for _, p := range rows {
			results = append(results, placeRowToGeocodeResult(p))
		}
	}
	writeGeocode(w, results, "")
}

func placeRowToGeocodeResult(p store.PlaceRow) gapi.GeocodeResult {
	return gapi.GeocodeResult{
		AddressComponents: []gapi.AddressComponent{},
		FormattedAddress:  p.Names["default"] + ", " + formatAddress(p.Address),
		Geometry: gapi.GeocodeGeometry{
			Location:     gapi.GeoLatLng{Lat: p.Lat, Lng: p.Lon},
			LocationType: "GEOMETRIC_CENTER",
		},
		PlaceID: p.PlaceID,
		Types:   p.Categories,
	}
}
```

(`strings`/`strconv` 此时真正用上;Task 6 若删过 import 现在加回。)

- [ ] **Step 4: 跑测试确认通过**(8 个 geocode 测试)+ 全包回归 + gofmt/vet

- [ ] **Step 5: Commit**

```bash
git add gateway/
git commit -m "feat: reverse geocode with nominatim address + nearby POIs"
```

---

### Task 8:接线 + golden + README + e2e

**Files:**
- Modify: `gateway/cmd/gateway/main.go`, `golden/cases.yaml`, `golden/run_golden.py`, `README.md`

- [ ] **Step 1: main.go 接线**

```go
// import 增加:
	"github.com/danielnanuk/open-map-service/gateway/internal/geocode"
// main() 中:
	nmURL := env("NOMINATIM_URL", "http://localhost:8081")
	h := httpapi.NewWithGeocoder(search.New(osURL), pg, geocode.NewNominatim(nmURL))
// 路由增加:
	mux.HandleFunc("GET /maps/api/geocode/json", h.Geocode)
```

docker-compose.yml 的 gateway 服务 environment 增加:
```yaml
      NOMINATIM_URL: http://nominatim:8080
```

- [ ] **Step 2: golden runner 支持 GET 端点**

`golden/run_golden.py` 修改:ENDPOINTS 增加 `"geocode": "/maps/api/geocode/json"`;`run_case` 开头改为:

```python
def run_case(case: dict) -> tuple[bool, str]:
    if case["endpoint"] == "geocode":
        r = requests.get(BASE + ENDPOINTS["geocode"], params=case["params"], timeout=10)
    else:
        r = requests.post(BASE + ENDPOINTS[case["endpoint"]], json=case["body"], timeout=10)
    if r.status_code != 200:
        return False, f"HTTP {r.status_code}: {r.text[:200]}"
    body = r.json()
    if case.get("expect_status"):
        if body.get("status") != case["expect_status"]:
            return False, f"status={body.get('status')} want {case['expect_status']}"
    ...(其余不变)
```

- [ ] **Step 3: golden 用例追加**(cases.yaml)

```yaml
- name: geocode_street_en
  endpoint: geocode
  params: {address: "Street 271, Phnom Penh"}
  expect_status: OK
  expect_any: ["271", "ផ្លូវ"]
- name: geocode_admin_khmer
  endpoint: geocode
  params: {address: "ខេត្តសៀមរាប"}
  expect_status: OK
  expect_any: ["Siem Reap", "សៀមរាប"]
- name: geocode_poi_routes_to_search
  endpoint: geocode
  params: {address: "Angkor Wat"}
  expect_status: OK
  expect_any: ["Angkor", "អង្គរ"]
- name: reverse_pp_riverside
  endpoint: geocode
  params: {latlng: "11.5621,104.9160"}
  expect_status: OK
  expect_any: ["Phnom Penh", "ភ្នំពេញ"]
```

若 `geocode_street_en` 因数据现实(OSM 柬埔寨该街道命名变体)不命中:先用 `curl localhost:8081/search?...` 取证,可凭证据替换为确认存在的街道(如 "Norodom Boulevard, Phnom Penh"),报告偏差。

- [ ] **Step 4: README 更新**

- 快速开始:`make up` 现在含 Nominatim,首次启动导入约 15-60 分钟,`docker logs places-nominatim-1` 看进度;就绪标志 `curl localhost:8081/status`
- 示例增加正/逆 geocode curl 各一条
- 已知差异追加:
  - geocode 结果中来自 Nominatim 的 `place_id` 形如 `nominatim:way:123`,不能用于 `/v1/places/{id}` 详情(两套数据域);OS 来源的结果可以
  - legacy Geocoding 形态无 attribution 字段,数据署名见 README 许可段(ODbL)

- [ ] **Step 5: e2e**

```bash
make test-go && docker compose up -d --build gateway
curl -s 'localhost:8080/maps/api/geocode/json?address=Norodom+Boulevard,+Phnom+Penh' | head -c 600
curl -s 'localhost:8080/maps/api/geocode/json?latlng=11.5621,104.9160' | head -c 800
make golden        # 12 cases(8 旧 + 4 新),0 hard failures
```

若 places 表被集成测试清空过,先 `make etl-load etl-conflate etl-index`。

- [ ] **Step 6: Commit**

```bash
git add gateway/ golden/ README.md docker-compose.yml
git commit -m "feat: geocode endpoint wired + golden geocode cases (M2 complete)"
```

---

## 自审记录(写完计划后跑过)

1. **Spec 覆盖(M2)**:Nominatim 接入 ✓(T1);正向 geocode + 分流(含门牌/道路/行政区模式优先 Nominatim、自由文本优先 OS、零结果互为回退)✓(T3/T6);逆向 = Nominatim 为主 + PostGIS 最近 POI 叠加 ✓(T5/T7);`GET /maps/api/geocode/json` 端点 ✓(T6/T8);验收"地址/行政区查询样本通过" ✓(T8 golden 4 用例)。spec §6 的"Geofabrik 每日增量复制"不在 M2 验收行内,归 M5 更新管道(README 注明导入是一次性)。
2. **占位符扫描**:无 TBD;T6 的 reverseGeocode 编译占位在 T7 被替换并有测试,属任务间衔接非占位;两处"实现笔误修正"(itoa/jsonEncode)都给出了最终代码。
3. **类型一致性**:`Geocoder` 接口签名 = `Nominatim` 方法集(T2/T6);`PlaceStore` 扩展后 fakeStore 补齐(T7);`GeoLatLng.lat/lng` 与测试断言键一致;`NewWithGeocoder(s, ps, g)` 三处调用一致;`geocode.AddressLike`/`NominatimToGeocodeResult`/`DocToGeocodeResult` 名称在 T3/T4/T6 一致。
