package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWorksheetSeparatesKeysAndEscapesAllContent(t *testing.T) {
	args := `{"title":"<script>title</script>","instructions":"Show your work.","exercises":[{"prompt":"<img src=https://invalid> 2 + 3?","answer":"UNIQUE_KEY_5","explanation":"<script>alert(1)</script>"}]}`
	a, err := Call("make_worksheet", json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Call("make_worksheet", json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	first, _ := json.Marshal(a)
	second, _ := json.Marshal(b)
	if string(first) != string(second) {
		t.Fatal("nondeterministic worksheet")
	}
	result := a.(map[string]any)
	sheet, key := result["html"].(string), result["answer_key_html"].(string)
	if strings.Contains(sheet, "UNIQUE_KEY_5") || !strings.Contains(key, "UNIQUE_KEY_5") {
		t.Fatal("key isolation")
	}
	for _, page := range []string{sheet, key} {
		if strings.Contains(page, "<script>") || strings.Contains(page, "<img ") || !strings.Contains(page, "&lt;script&gt;title") || !strings.Contains(page, "Content-Security-Policy") || !strings.Contains(page, "default-src 'none'") {
			t.Fatal("unsafe worksheet")
		}
	}
	if result["preview_requires_scripts"] != false || result["network_requests"] != false || result["offline"] != true || result["content_verified"] != false {
		t.Fatal("wrong safety flags")
	}
}

func TestWorksheetContractBounds(t *testing.T) {
	base := map[string]any{"title": "Arithmetic", "instructions": "Show working.", "exercises": []any{map[string]any{"prompt": "2+3?", "answer": "5", "explanation": "Two and three sum to five."}}}
	for _, field := range []string{"title", "instructions"} {
		copy := map[string]any{}
		for k, v := range base {
			copy[k] = v
		}
		copy[field] = " "
		raw, _ := json.Marshal(copy)
		if _, err := Call("make_worksheet", raw); err == nil {
			t.Fatal("blank accepted")
		}
	}
	for _, count := range []int{0, 31} {
		copy := map[string]any{}
		for k, v := range base {
			copy[k] = v
		}
		copy["exercises"] = make([]any, count)
		raw, _ := json.Marshal(copy)
		if _, err := Call("make_worksheet", raw); err == nil {
			t.Fatal("count accepted")
		}
	}
	for _, raw := range []string{`{"title":"x","instructions":"x","exercises":[{"prompt":"Q","answer":" ","explanation":"x"}]}`, `{"title":"x","instructions":"x","exercises":[{"prompt":"Q","answer":"x","explanation":" "}]}`, `{"title":"x","instructions":"x","exercises":[{"prompt":"Q","answer":"x","explanation":"x","script":"x"}]}`} {
		if _, err := Call("make_worksheet", json.RawMessage(raw)); err == nil {
			t.Fatal("invalid field accepted")
		}
	}
}
