package studyhost

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testToken = "study-origin-test-credential-32-bytes"

func fixture(count int) string {
	questions := make([]map[string]any, count)
	for i := range questions {
		questions[i] = map[string]any{"prompt": "What is 2 + 3?", "choices": []string{"4", "5", "6"}, "correct_index": 1, "explanation": "Adding 2 and 3 gives 5."}
	}
	data, _ := json.Marshal(map[string]any{"tool": ToolName, "arguments": map[string]any{"title": "Arithmetic practice", "questions": questions}})
	return string(data)
}

func request(handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func handler(t *testing.T) http.Handler {
	t.Helper()
	h, err := New(testToken)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestCredentialFailsClosed(t *testing.T) {
	for _, value := range []string{"", strings.Repeat("x", 31), strings.Repeat("x", 257), strings.Repeat("x", 32) + " ", strings.Repeat("x", 32) + "\x00", strings.Repeat("é", 32)} {
		if _, err := New(value); err == nil {
			t.Fatal("unsafe credential accepted")
		}
	}
}

func TestActualStudyRenderAndDeterministicReplay(t *testing.T) {
	for _, count := range []int{1, 3, 5, 30} {
		h := handler(t)
		first := request(h, "POST", "/v1/tools/call", fixture(count), testToken)
		if first.Code != 200 {
			t.Fatalf("%d-question render: %d", count, first.Code)
		}
		second := request(h, "POST", "/v1/tools/call", fixture(count), testToken)
		if !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
			t.Fatal("replay changed deterministic output")
		}
		var response struct {
			OK     bool           `json:"ok"`
			Tool   string         `json:"tool"`
			Result map[string]any `json:"result"`
		}
		if json.Unmarshal(first.Body.Bytes(), &response) != nil || !response.OK || response.Tool != ToolName {
			t.Fatal("invalid envelope")
		}
		r := response.Result
		if r["filename"] != "study-app.html" || r["media_type"] != "text/html; charset=utf-8" || r["offline"] != true || r["network_requests"] != false || r["content_verified"] != false || r["preview_requires_scripts"] != true {
			t.Fatal("incorrect export safety contract")
		}
		html := r["html"].(string)
		if !strings.HasPrefix(html, "<!doctype html>") || !strings.Contains(html, "Arithmetic practice") || !strings.Contains(html, "Retry missed") {
			t.Fatal("missing study artifact")
		}
		if first.Header().Get("Cache-Control") != "no-store" || first.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("missing privacy headers")
		}
	}
}

func TestRejectsOtherToolsAndMalformedInput(t *testing.T) {
	cases := []struct {
		method, path, body, token string
		status                    int
	}{
		{"POST", "/v1/tools/call", fixture(1), "", 401},
		{"POST", "/v1/tools/call", fixture(1), strings.Repeat("z", 32), 401},
		{"GET", "/v1/tools", "", testToken, 404},
		{"POST", "/v1/tools/make_study_app", fixture(1), testToken, 404},
		{"POST", "/v1/sessions", "", testToken, 404},
		{"GET", "/v1/tools/call", "", testToken, 405},
		{"POST", "/v1/tools/call?token=x", fixture(1), testToken, 404},
		{"POST", "/v1/tools/call?", fixture(1), testToken, 404},
		{"POST", "/v1/tools/%63all", fixture(1), testToken, 404},
		{"POST", "/v1/tools/call", strings.Replace(fixture(1), ToolName, "calculate", 1), testToken, 400},
		{"POST", "/v1/tools/call", fixture(0), testToken, 400},
		{"POST", "/v1/tools/call", fixture(31), testToken, 400},
		{"POST", "/v1/tools/call", "null", testToken, 400},
		{"POST", "/v1/tools/call", fixture(1) + " {}", testToken, 400},
		{"POST", "/v1/tools/call", `{"tool":"calculate","tool":"make_study_app","arguments":{}}`, testToken, 400},
		{"POST", "/v1/tools/call", strings.Replace(fixture(1), `"correct_index":1`, `"correct_index":5`, 1), testToken, 400},
		{"POST", "/v1/tools/call", strings.Replace(fixture(1), `"choices":["4","5","6"]`, `"choices":["5"," 5 "]`, 1), testToken, 400},
		{"POST", "/v1/tools/call", strings.Repeat(" ", MaxRequestBytes+1), testToken, 413},
	}
	for _, c := range cases {
		w := request(handler(t), c.method, c.path, c.body, c.token)
		if w.Code != c.status {
			t.Errorf("%s %s: got %d want %d", c.method, c.path, w.Code, c.status)
		}
		if strings.Contains(w.Body.String(), testToken) || strings.Contains(w.Body.String(), "What is") {
			t.Fatal("error echoed private data")
		}
	}
	for _, contentType := range []string{"text/plain", "application/json;foo=bar", "application/json;charset=ascii"} {
		r := httptest.NewRequest("POST", "/v1/tools/call", strings.NewReader(fixture(1)))
		r.Header.Set("Authorization", "Bearer "+testToken)
		r.Header.Set("Content-Type", contentType)
		w := httptest.NewRecorder()
		handler(t).ServeHTTP(w, r)
		if w.Code != 415 {
			t.Fatal("unsafe content type accepted")
		}
	}
	r := httptest.NewRequest("POST", "/v1/tools/call", strings.NewReader(fixture(1)))
	r.Header.Add("Authorization", "Bearer "+testToken)
	r.Header.Add("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	handler(t).ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("ambiguous authorization accepted")
	}
}

func TestHealthAndUnauthorizedTrafficCannotConsumeCapacity(t *testing.T) {
	h := handler(t)
	for i := 0; i < 100; i++ {
		if request(h, "GET", "/health", "", "").Code != 200 || request(h, "POST", "/v1/tools/call", fixture(1), "").Code != 401 {
			t.Fatal("unexpected probe response")
		}
	}
	for i := 0; i < RequestsPerMinute; i++ {
		if request(h, "POST", "/v1/tools/call", fixture(1), testToken).Code != 200 {
			t.Fatal("tool capacity consumed by probes")
		}
	}
	w := request(h, "POST", "/v1/tools/call", fixture(1), testToken)
	if w.Code != 429 || w.Header().Get("Retry-After") != "60" {
		t.Fatal("global origin rate limit missing")
	}
}
