package fieldmask

import (
	"encoding/json"
	"reflect"
	"testing"
)

func apply(t *testing.T, doc, mask string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatal(err)
	}
	return Apply(m, mask)
}

func TestApplyPrunesNestedAndArrays(t *testing.T) {
	doc := `{"places":[{"id":"a","displayName":{"text":"X"},"location":{"latitude":1}},
	                   {"id":"b","displayName":{"text":"Y"},"location":{"latitude":2}}]}`
	got := apply(t, doc, "places.id,places.displayName")
	want := map[string]any{"places": []any{
		map[string]any{"id": "a", "displayName": map[string]any{"text": "X"}},
		map[string]any{"id": "b", "displayName": map[string]any{"text": "Y"}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestStarReturnsAll(t *testing.T) {
	doc := `{"places":[{"id":"a"}]}`
	if got := apply(t, doc, "*"); len(got["places"].([]any)) != 1 {
		t.Fatalf("star mask should keep everything, got %v", got)
	}
}

func TestEmptyMaskReturnsAll(t *testing.T) {
	doc := `{"places":[{"id":"a"}]}`
	if got := apply(t, doc, ""); len(got["places"].([]any)) != 1 {
		t.Fatalf("empty mask should keep everything, got %v", got)
	}
}

func TestUnknownPathsAndWhitespace(t *testing.T) {
	doc := `{"places":[{"id":"a","types":["cafe"]}]}`
	got := apply(t, doc, " places.id , places.nonexistent ")
	want := map[string]any{"places": []any{map[string]any{"id": "a"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
