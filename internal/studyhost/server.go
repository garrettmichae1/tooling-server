// Package studyhost is the deliberately narrow HTTP entry point for the hosted
// study pilot. The existing localhost figure server is unchanged.
package studyhost

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"edsger.local/figureserver/internal/auth"
	"edsger.local/figureserver/internal/ratelimit"
	"edsger.local/figureserver/internal/tools"
)

const (
	MaxRequestBytes   = 262_144
	RequestsPerMinute = 30
	ToolName          = "make_study_app"
)

// New exposes only health and one bounded utility. No renderer, subprocess,
// catalog, filesystem operation, or other tool is reachable through this host.
func New(token string) (http.Handler, error) {
	return newHost(token, ToolName)
}

// NewWorksheet keeps free rendering isolated from the quiz origin. A process
// serves exactly one allowlisted tool, even when its shared binary contains more.
func NewWorksheet(token string) (http.Handler, error) {
	return newHost(token, "make_worksheet")
}

func newHost(token, toolName string) (http.Handler, error) {
	if len(token) < 32 || len(token) > 256 {
		return nil, errors.New("invalid_origin_credential")
	}
	for _, b := range []byte(token) {
		if b < 0x21 || b > 0x7e {
			return nil, errors.New("invalid_origin_credential")
		}
	}
	limit := ratelimit.New(RequestsPerMinute)
	active := make(chan struct{}, 1)
	handler := auth.Middleware(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.EscapedPath() != r.URL.Path {
			failure(w, http.StatusNotFound, "not_found")
			return
		}
		if r.URL.Path == "/health" && r.Method == http.MethodGet {
			respond(w, http.StatusOK, map[string]any{"status": "ok", "service": "edsger-study-container", "tool": toolName})
			return
		}
		if r.URL.Path != "/v1/tools/call" {
			failure(w, http.StatusNotFound, "not_found")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			failure(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		if !limit.Allow(time.Now()) {
			w.Header().Set("Retry-After", "60")
			failure(w, http.StatusTooManyRequests, "rate_limited")
			return
		}
		select {
		case active <- struct{}{}:
			defer func() { <-active }()
		default:
			w.Header().Set("Retry-After", "1")
			failure(w, http.StatusTooManyRequests, "busy")
			return
		}
		media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" || len(params) > 1 || (len(params) == 1 && params["charset"] != "utf-8") || r.Header.Get("Content-Encoding") != "" {
			failure(w, http.StatusUnsupportedMediaType, "unsupported_media_type")
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxRequestBytes))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				failure(w, http.StatusRequestEntityTooLarge, "payload_too_large")
			} else {
				failure(w, http.StatusBadRequest, "invalid_request")
			}
			return
		}
		normalized, err := tools.NormalizeJSON(raw)
		if err != nil {
			failure(w, http.StatusBadRequest, "invalid_request")
			return
		}
		var call struct {
			Tool      string          `json:"tool"`
			Arguments json.RawMessage `json:"arguments"`
		}
		decoder := json.NewDecoder(bytes.NewReader(normalized))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&call) != nil || decoder.Decode(new(any)) != io.EOF || call.Tool != toolName {
			failure(w, http.StatusBadRequest, "invalid_request")
			return
		}
		var args struct {
			Questions []json.RawMessage `json:"questions"`
			Exercises []json.RawMessage `json:"exercises"`
		}
		if json.Unmarshal(call.Arguments, &args) != nil {
			failure(w, http.StatusBadRequest, "invalid_request")
			return
		}
		count := len(args.Questions)
		if toolName == "make_worksheet" {
			count = len(args.Exercises)
		}
		if count < 1 || count > 30 {
			failure(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if r.Context().Err() != nil {
			failure(w, http.StatusRequestTimeout, "request_cancelled")
			return
		}
		result, err := tools.Call(toolName, call.Arguments)
		if err != nil {
			// Never return supplied question text or internal errors from this host.
			failure(w, http.StatusBadRequest, "invalid_request")
			return
		}
		respond(w, http.StatusOK, map[string]any{"ok": true, "tool": toolName, "result": result})
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.URL.Path != "/health" && len(r.Header.Values("Authorization")) != 1 {
			failure(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		handler.ServeHTTP(w, r)
	}), nil
}

func failure(w http.ResponseWriter, status int, code string) {
	respond(w, status, map[string]any{"ok": false, "error": code, "code": code})
}

func respond(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
