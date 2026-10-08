package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"edsger.local/figureserver/internal/httpapi"
	"edsger.local/figureserver/internal/tools"
)

func utilityServer(t *testing.T, maxBody int64, rate int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(httpapi.New(token, maxBody, rate, nil, nil, nil, nil, "guide", nil))
	t.Cleanup(srv.Close)
	return srv
}
func utilityRequest(t *testing.T, srv *httptest.Server, method, path, payload, credential string) (int, http.Header, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("invalid JSON %d: %s", res.StatusCode, raw)
	}
	return res.StatusCode, res.Header, result
}

func TestUtilityCatalogAndAllCalls(t *testing.T) {
	srv := utilityServer(t, 256*1024, 1000)
	status, _, catalog := utilityRequest(t, srv, "GET", "/v1/tools", "", token)
	if status != 200 || len(catalog["tools"].([]any)) != 39 {
		t.Fatal(status, catalog)
	}
	renderers := catalog["renderers"].(map[string]any)
	if renderers["posters"] != false {
		t.Fatal("tools-only advertises renderer")
	}
	status, _, category := utilityRequest(t, srv, "GET", "/v1/tools?category=study", "", token)
	if status != 200 || len(category["tools"].([]any)) != 5 {
		t.Fatal(category)
	}
	for _, definition := range tools.Catalog("") {
		t.Run(definition.Name, func(t *testing.T) {
			status, _, result := utilityRequest(t, srv, "GET", "/v1/tools/"+definition.Name, "", token)
			if status != 200 || result["name"] != definition.Name {
				t.Fatal(status, result)
			}
			status, _, result = utilityRequest(t, srv, "POST", "/v1/tools/"+definition.Name, string(definition.Example), token)
			if status != 200 || result["ok"] != true {
				t.Fatal(status, result)
			}
			payload, _ := json.Marshal(map[string]any{"tool": definition.Name, "arguments": definition.Example})
			status, _, result = utilityRequest(t, srv, "POST", "/v1/tools/call", string(payload), token)
			if status != 200 || result["ok"] != true {
				t.Fatal(status, result)
			}
		})
	}
	for _, path := range []string{"/v1/sessions", "/v1/handouts", "/v1/charts", "/v1/models", "/v1/molecules"} {
		status, _, _ := utilityRequest(t, srv, "POST", path, `{}`, token)
		if status != 503 {
			t.Fatalf("%s returned %d", path, status)
		}
	}
}

func TestUtilityErrorsAndProtection(t *testing.T) {
	srv := utilityServer(t, 256*1024, 1000)
	for _, path := range []string{"/v1/tools", "/v1/tools/calculate", "/v1/tools/call"} {
		status, _, _ := utilityRequest(t, srv, "POST", path, `{}`, "")
		if status != 401 {
			t.Fatalf("unprotected %s: %d", path, status)
		}
	}
	for _, tc := range []struct {
		path, body, code string
		status           int
	}{
		{"/v1/tools/call", `{"tool":"calculate","arguments":{"expression":"1/0"}}`, "invalid_arguments", 400},
		{"/v1/tools/call", `{"tool":"calculate","arguments":{"expression":"2"},"extra":1}`, "invalid_request", 400},
		{"/v1/tools/call", `{"tool":"calculate","tool":"date_math","arguments":{}}`, "invalid_request", 400},
		{"/v1/tools/call", `{"tool":"calculate","arguments":null}`, "invalid_arguments", 400},
		{"/v1/tools/call", `{"tool":"calculate","arguments":{"expression":"2"}}{}`, "invalid_request", 400},
		{"/v1/tools/nope", `{}`, "unknown_tool", 404},
		{"/v1/tools/calculate", `{"expression":1}`, "invalid_arguments", 400},
	} {
		status, headers, result := utilityRequest(t, srv, "POST", tc.path, tc.body, token)
		if status != tc.status || result["code"] != tc.code || result["ok"] != false {
			t.Fatal(status, result)
		}
		if headers.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("missing secure headers")
		}
	}
	status, _, result := utilityRequest(t, srv, "POST", "/v1/tools/calculate", `{"expression":"1/0"}`, token)
	if status != 400 || result["field"] != "arguments.expression" {
		t.Fatal(result)
	}
	small := utilityServer(t, 64, 1000)
	status, _, result = utilityRequest(t, small, "POST", "/v1/tools/calculate", strings.Repeat("x", 65), token)
	if status != 413 || result["code"] != "body_too_large" {
		t.Fatal(status, result)
	}
}

func TestAuthAndHealthDoNotConsumeToolBudget(t *testing.T) {
	srv := utilityServer(t, 256*1024, 1)
	for i := 0; i < 3; i++ {
		status, _, _ := utilityRequest(t, srv, "GET", "/health", "", "")
		if status != 200 {
			t.Fatal(status)
		}
		status, _, _ = utilityRequest(t, srv, "GET", "/v1/tools", "", "wrong")
		if status != 401 {
			t.Fatal(status)
		}
	}
	status, _, _ := utilityRequest(t, srv, "POST", "/v1/tools/calculate", `{"expression":"2+3"}`, token)
	if status != 200 {
		t.Fatalf("budget exhausted by unauthenticated traffic: %d", status)
	}
	status, headers, _ := utilityRequest(t, srv, "GET", "/v1/tools", "", token)
	if status != 429 || headers.Get("Retry-After") == "" {
		t.Fatal("missing rate limit", status)
	}
	status, _, _ = utilityRequest(t, srv, "GET", "/health", "", "")
	if status != 200 {
		t.Fatal("health blocked by exhausted budget")
	}
}
