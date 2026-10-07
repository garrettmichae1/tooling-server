package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"edsger.local/figureserver/internal/tools"
)

func (s *Server) catalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version": "1.0", "tools": tools.Catalog(r.URL.Query().Get("category")),
		"call_endpoint": "POST /v1/tools/call", "guide_endpoint": "GET /v1/guide",
		"limits":    map[string]any{"max_body_bytes": s.maxBody, "requests_per_minute": s.ratePerMinute},
		"renderers": map[string]bool{"posters": s.sessions != nil, "handouts": s.handouts != nil, "charts": s.charts != nil, "objects": s.models != nil, "molecules": s.models != nil},
	})
}

func (s *Server) toolDefinition(w http.ResponseWriter, r *http.Request) {
	definition, ok := tools.Lookup(r.PathValue("name"))
	if !ok {
		writeToolErr(w, http.StatusNotFound, r.PathValue("name"), &tools.Error{Code: "unknown_tool", Message: "unknown tool; choose a name from GET /v1/tools", Field: "tool"})
		return
	}
	writeJSON(w, http.StatusOK, definition)
}

func (s *Server) toolCall(w http.ResponseWriter, r *http.Request) {
	raw, ok := s.readToolBody(w, r)
	if !ok {
		return
	}
	var in struct {
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}
	normalized, parseErr := tools.NormalizeJSON(raw)
	if parseErr != nil {
		writeToolErr(w, http.StatusBadRequest, "", &tools.Error{Code: "invalid_request", Message: parseErr.Error()})
		return
	}
	dec := json.NewDecoder(bytes.NewReader(normalized))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || dec.Decode(new(any)) != io.EOF {
		writeToolErr(w, http.StatusBadRequest, "", &tools.Error{Code: "invalid_request", Message: "send {tool: name, arguments: object}; no extra fields or trailing JSON"})
		return
	}
	s.runTool(w, in.Tool, in.Arguments)
}

func (s *Server) directTool(w http.ResponseWriter, r *http.Request) {
	raw, ok := s.readToolBody(w, r)
	if !ok {
		return
	}
	s.runTool(w, r.PathValue("name"), raw)
}

func (s *Server) readToolBody(w http.ResponseWriter, r *http.Request) (json.RawMessage, bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.maxBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeToolErr(w, http.StatusRequestEntityTooLarge, "", &tools.Error{Code: "body_too_large", Message: "request body exceeds server limit"})
		} else {
			writeToolErr(w, http.StatusBadRequest, "", &tools.Error{Code: "invalid_request", Message: "could not read request body"})
		}
		return nil, false
	}
	return raw, true
}

func (s *Server) runTool(w http.ResponseWriter, name string, raw json.RawMessage) {
	result, err := tools.Call(name, raw)
	if err != nil {
		var detail *tools.Error
		if !errors.As(err, &detail) {
			detail = &tools.Error{Code: "tool_failed", Message: "tool could not complete the request"}
		}
		code := http.StatusBadRequest
		if detail.Code == "unknown_tool" {
			code = http.StatusNotFound
		}
		if detail.Code == "result_too_large" || detail.Code == "numeric_range" {
			code = http.StatusUnprocessableEntity
		}
		if detail.Code == "tool_failed" {
			code = http.StatusServiceUnavailable
		}
		writeToolErr(w, code, name, detail)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tool": name, "result": result})
}

func writeToolErr(w http.ResponseWriter, status int, name string, detail *tools.Error) {
	writeJSON(w, status, map[string]any{"ok": false, "tool": name, "error": detail.Message, "code": detail.Code, "field": detail.Field})
}
