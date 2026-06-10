package search

import "testing"

func TestSearchTextBodyWithBias(t *testing.T) {
	body := searchTextBody("noodle", &Geo{Lat: 11.5, Lon: 104.9}, 10)
	fs := body["query"].(map[string]any)["function_score"].(map[string]any)
	mm := fs["query"].(map[string]any)["multi_match"].(map[string]any)
	if mm["query"] != "noodle" {
		t.Fatalf("query text lost: %v", mm)
	}
	if len(fs["functions"].([]map[string]any)) != 2 { // confidence + gauss
		t.Fatalf("want 2 scoring functions, got %v", fs["functions"])
	}
	if body["size"] != 10 {
		t.Fatalf("size: %v", body["size"])
	}
}

func TestSearchTextBodyNoBias(t *testing.T) {
	body := searchTextBody("noodle", nil, 5)
	fs := body["query"].(map[string]any)["function_score"].(map[string]any)
	if len(fs["functions"].([]map[string]any)) != 1 { // 仅 confidence
		t.Fatalf("want 1 scoring function, got %v", fs["functions"])
	}
}

func TestNearbyBodyDistanceRank(t *testing.T) {
	body := nearbyBody(Geo{Lat: 11.5, Lon: 104.9}, 500, []string{"restaurant"}, 20, true)
	filters := body["query"].(map[string]any)["bool"].(map[string]any)["filter"].([]map[string]any)
	if len(filters) != 2 { // geo_distance + terms
		t.Fatalf("filters: %v", filters)
	}
	if _, ok := body["sort"]; !ok {
		t.Fatal("DISTANCE rank must set sort by _geo_distance")
	}
}

func TestNearbyBodyPopularityRank(t *testing.T) {
	body := nearbyBody(Geo{Lat: 11.5, Lon: 104.9}, 500, nil, 20, false)
	if _, ok := body["sort"]; ok {
		t.Fatal("POPULARITY rank must not set explicit sort")
	}
	if _, ok := body["query"].(map[string]any)["function_score"]; !ok {
		t.Fatal("POPULARITY rank should wrap in function_score")
	}
}
