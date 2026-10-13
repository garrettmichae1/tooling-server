package httpapi_test

import (
	"encoding/json"
	"testing"
)

func TestWordSearchHTTPBoundaryAndReplay(t *testing.T) {
	srv := utilityServer(t, 256*1024, 1000)
	call := `{"tool":"make_word_search","arguments":{"title":"Animals","words":["CAT","DOG","BIRD"],"difficulty":"easy","size":8.0,"seed":1e1}}`
	status, _, first := utilityRequest(t, srv, "POST", "/v1/tools/call", call, token)
	if status != 200 || first["tool"] != "make_word_search" || first["ok"] != true {
		t.Fatal("word-search HTTP call failed", status)
	}
	status, _, second := utilityRequest(t, srv, "POST", "/v1/tools/call", call, token)
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if status != 200 || string(a) != string(b) {
		t.Fatal("same HTTP request changed the puzzle")
	}
	result := first["result"].(map[string]any)
	if result["seed"] != float64(10) || result["size"] != float64(8) || result["puzzle_verified"] != true || result["content_verified"] != false || len(result["placements"].([]any)) != 3 {
		t.Fatal("HTTP contract differs from the declared native contract")
	}
	for _, example := range []struct {
		body   string
		status int
		code   string
	}{
		{`{"tool":"make_word_search","arguments":{"title":"Invalid","words":["CAT","TAC"],"difficulty":"easy"}}`, 400, "invalid_arguments"},
		{`{"tool":"make_word_search","arguments":{"title":"Invalid","words":["CAT"],"difficulty":"easy","size":8.5}}`, 400, "invalid_arguments"},
		{`{"tool":"make_word_search","arguments":{"title":"Impossible","words":["AAA","AAAA"],"difficulty":"easy"}}`, 422, "generation_failed"},
	} {
		status, _, failure := utilityRequest(t, srv, "POST", "/v1/tools/call", example.body, token)
		if status != example.status || failure["code"] != example.code || failure["ok"] != false {
			t.Fatal("incorrect public utility error", status, failure["code"])
		}
		if _, exists := failure["result"]; exists {
			t.Fatal("failed generation exposed a partial puzzle")
		}
	}
}
