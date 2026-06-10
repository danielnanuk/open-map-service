package search

import "strconv"

type Geo struct{ Lat, Lon float64 }

// ★ Overture 实测多语言 common names 为空、主名(含高棉文)都在 name_default,
// 因此 latn 转写字段在 name_default 与 name_km 上都要参与检索。
var textFields = []string{"name_default^3", "name_en^3", "name_km^3", "name_zh^3",
	"name_default.latn^2", "name_km.latn^2", "formatted_address"}
var acFields = []string{"name_default.ac", "name_en.ac", "name_km.ac", "name_zh.ac",
	"name_default.latn_ac", "name_km.latn_ac"}

func confidenceFn() map[string]any {
	return map[string]any{"field_value_factor": map[string]any{"field": "confidence", "missing": 0.7}}
}

func gaussFn(g Geo) map[string]any {
	return map[string]any{"gauss": map[string]any{"location": map[string]any{
		"origin": map[string]any{"lat": g.Lat, "lon": g.Lon}, "scale": "5km", "decay": 0.5}}}
}

func functionScore(query map[string]any, bias *Geo) map[string]any {
	fns := []map[string]any{confidenceFn()}
	if bias != nil {
		fns = append(fns, gaussFn(*bias))
	}
	return map[string]any{"function_score": map[string]any{
		"query": query, "functions": fns, "score_mode": "multiply", "boost_mode": "multiply"}}
}

func searchTextBody(q string, bias *Geo, size int) map[string]any {
	mm := map[string]any{"multi_match": map[string]any{"query": q, "type": "best_fields", "fields": textFields}}
	return map[string]any{"size": size, "query": functionScore(mm, bias)}
}

func autocompleteBody(input string, bias *Geo, size int) map[string]any {
	mm := map[string]any{"multi_match": map[string]any{"query": input, "type": "best_fields", "fields": acFields}}
	return map[string]any{"size": size, "query": functionScore(mm, bias)}
}

func nearbyBody(center Geo, radiusMeters float64, types []string, size int, rankByDistance bool) map[string]any {
	filters := []map[string]any{{"geo_distance": map[string]any{
		"distance": fmtMeters(radiusMeters),
		"location": map[string]any{"lat": center.Lat, "lon": center.Lon}}}}
	if len(types) > 0 {
		filters = append(filters, map[string]any{"terms": map[string]any{"categories": types}})
	}
	boolq := map[string]any{"bool": map[string]any{"filter": filters}}
	body := map[string]any{"size": size}
	if rankByDistance {
		body["query"] = boolq
		body["sort"] = []map[string]any{{"_geo_distance": map[string]any{
			"location": map[string]any{"lat": center.Lat, "lon": center.Lon},
			"order":    "asc", "unit": "m"}}}
	} else {
		body["query"] = functionScore(boolq, &center)
	}
	return body
}

func fmtMeters(m float64) string { // ★ 修正计划稿中的笔误,直接用 strconv
	return strconv.FormatFloat(m, 'f', -1, 64) + "m"
}
