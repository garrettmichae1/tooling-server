// Package httpapi is the localhost HTTP front door for figure sessions.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"edsger.local/figureserver/docs"
	"edsger.local/figureserver/internal/allowlist"
	"edsger.local/figureserver/internal/auth"
	"edsger.local/figureserver/internal/chart"
	"edsger.local/figureserver/internal/handout"
	"edsger.local/figureserver/internal/model"
	"edsger.local/figureserver/internal/molecule"
	"edsger.local/figureserver/internal/ratelimit"
	"edsger.local/figureserver/internal/session"
)

var sessionID = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Server serves the figure API.
type Server struct {
	token         string
	maxBody       int64
	sessions      *session.Manager
	handouts      *handout.Compiler
	charts        *chart.Compiler
	models        *model.Compiler
	guide         string
	limit         *ratelimit.Limiter
	ratePerMinute int
	renderSlots   chan struct{}
	log           *slog.Logger
	mux           *http.ServeMux
}

// New builds the handler. guide is the transcription document served at GET /v1/guide.
func New(token string, maxBody int64, ratePerMinute int, sessions *session.Manager, handouts *handout.Compiler, charts *chart.Compiler, models *model.Compiler, guide string, log *slog.Logger) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	if maxBody <= 0 {
		maxBody = 256 * 1024
	}
	s := &Server{
		token:         token,
		maxBody:       maxBody,
		sessions:      sessions,
		handouts:      handouts,
		charts:        charts,
		models:        models,
		guide:         guide,
		limit:         ratelimit.New(ratePerMinute),
		ratePerMinute: ratePerMinute,
		renderSlots:   make(chan struct{}, 2),
		log:           log,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /v1/guide", s.guideHandler)
	mux.HandleFunc("GET /v1/quickstart", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = io.WriteString(w, docs.Quickstart)
	})
	mux.HandleFunc("GET /v1/tools", s.catalog)
	mux.HandleFunc("GET /v1/tools/{name}", s.toolDefinition)
	mux.HandleFunc("POST /v1/tools/call", s.toolCall)
	mux.HandleFunc("POST /v1/tools/{name}", s.directTool)
	mux.HandleFunc("POST /v1/sessions", s.create)
	mux.HandleFunc("POST /v1/sessions/{id}/calls", s.call)
	mux.HandleFunc("POST /v1/sessions/{id}/export", s.export)
	mux.HandleFunc("DELETE /v1/sessions/{id}", s.delete)
	mux.HandleFunc("POST /v1/handouts", s.withRenderSlot(s.handout))
	mux.HandleFunc("POST /v1/charts", s.withRenderSlot(s.chart))
	mux.HandleFunc("POST /v1/models", s.withRenderSlot(s.model))
	mux.HandleFunc("POST /v1/molecules", s.withRenderSlot(s.molecule))
	s.mux = mux
	// Unauthorized traffic and health probes must not exhaust tool capacity.
	protected := auth.Middleware(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" && !s.limit.Allow(time.Now()) {
			w.Header().Set("Retry-After", "60")
			writeErr(w, http.StatusTooManyRequests, "too many requests")
			return
		}
		mux.ServeHTTP(w, r)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secureHeaders(w)
		protected.ServeHTTP(w, r)
	})
}

// A process-wide cap also covers the previously uncapped PDF/STL endpoints.
func (s *Server) withRenderSlot(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case s.renderSlots <- struct{}{}:
			defer func() { <-s.renderSlots }()
			next(w, r)
		default:
			w.Header().Set("Retry-After", "1")
			writeErr(w, http.StatusTooManyRequests, "render capacity reached; retry later")
		}
	}
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) guideHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, s.guide)
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		writeErr(w, http.StatusServiceUnavailable, "renderer unavailable")
		return
	}
	if !emptyBody(w, r) {
		return
	}
	started := time.Now()
	sess, err := s.sessions.Create(r.Context())
	if err != nil {
		s.log.Info("session_create", "status", "error", "ms", time.Since(started).Milliseconds())
		writeSessionErr(w, err)
		return
	}
	s.log.Info("session_create", "session", sess.ID(), "status", "ok", "ms", time.Since(started).Milliseconds())
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         sess.ID(),
		"expires_at": sess.Expires().UTC().Format(time.RFC3339),
		"tools":      allowlist.Names(),
	})
}

func (s *Server) call(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		writeErr(w, http.StatusServiceUnavailable, "renderer unavailable")
		return
	}
	id, ok := sessionParam(w, r)
	if !ok {
		return
	}
	body := http.MaxBytesReader(w, r.Body, s.maxBody)
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req struct {
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	var extra struct{}
	if err := dec.Decode(&extra); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := allowlist.Validate(req.Tool, req.Arguments); err != nil {
		writeErr(w, http.StatusBadRequest, "rejected")
		return
	}
	started := time.Now()
	result, err := s.sessions.Call(r.Context(), id, req.Tool, req.Arguments)
	status := "ok"
	if err != nil {
		status = "error"
	}
	s.log.Info("session_call", "session", id, "tool", req.Tool, "status", status, "ms", time.Since(started).Milliseconds())
	if err != nil {
		writeSessionErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": json.RawMessage(result)})
}

func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		writeErr(w, http.StatusServiceUnavailable, "renderer unavailable")
		return
	}
	id, ok := sessionParam(w, r)
	if !ok {
		return
	}
	if !emptyBody(w, r) {
		return
	}
	started := time.Now()
	png, err := s.sessions.Export(r.Context(), id)
	status := "ok"
	if err != nil {
		status = "error"
	}
	s.log.Info("session_export", "session", id, "status", status, "bytes", len(png), "ms", time.Since(started).Milliseconds())
	if err != nil {
		writeSessionErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="figure.png"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}

func (s *Server) handout(w http.ResponseWriter, r *http.Request) {
	if s.handouts == nil {
		writeErr(w, http.StatusServiceUnavailable, "renderer unavailable")
		return
	}
	body := http.MaxBytesReader(w, r.Body, s.maxBody)
	raw, err := io.ReadAll(body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	started := time.Now()
	pdf, err := s.handouts.Compile(r.Context(), raw)
	status := "ok"
	if err != nil {
		status = "error"
	}
	s.log.Info("handout", "status", status, "bytes", len(pdf), "ms", time.Since(started).Milliseconds())
	if err != nil {
		writeHandoutErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="handout.pdf"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdf)
}

func (s *Server) chart(w http.ResponseWriter, r *http.Request) {
	if s.charts == nil {
		writeErr(w, http.StatusServiceUnavailable, "renderer unavailable")
		return
	}
	body := http.MaxBytesReader(w, r.Body, s.maxBody)
	raw, err := io.ReadAll(body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	started := time.Now()
	pdf, err := s.charts.Compile(r.Context(), raw)
	status := "ok"
	if err != nil {
		status = "error"
	}
	s.log.Info("chart", "status", status, "bytes", len(pdf), "ms", time.Since(started).Milliseconds())
	if err != nil {
		writeChartErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="chart.pdf"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdf)
}

func writeChartErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, chart.ErrBrief):
		writeErr(w, http.StatusBadRequest, "rejected")
	case errors.Is(err, chart.ErrCompile):
		writeErr(w, http.StatusUnprocessableEntity, "chart rejected")
	case errors.Is(err, chart.ErrTooLarge):
		writeErr(w, http.StatusUnprocessableEntity, "result too large")
	default:
		writeErr(w, http.StatusServiceUnavailable, "renderer unavailable")
	}
}

func (s *Server) model(w http.ResponseWriter, r *http.Request) {
	if s.models == nil {
		writeErr(w, http.StatusServiceUnavailable, "renderer unavailable")
		return
	}
	body := http.MaxBytesReader(w, r.Body, s.maxBody)
	raw, err := io.ReadAll(body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	started := time.Now()
	stl, err := s.models.Compile(r.Context(), raw)
	status := "ok"
	if err != nil {
		status = "error"
	}
	s.log.Info("model", "status", status, "bytes", len(stl), "ms", time.Since(started).Milliseconds())
	if err != nil {
		writeModelErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "model/stl")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="model.stl"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(stl)
}

func (s *Server) molecule(w http.ResponseWriter, r *http.Request) {
	if s.models == nil {
		writeErr(w, http.StatusServiceUnavailable, "renderer unavailable")
		return
	}
	body := http.MaxBytesReader(w, r.Body, s.maxBody)
	raw, err := io.ReadAll(body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	started := time.Now()
	stl, err := molecule.Build(r.Context(), raw, *s.models)
	status := "ok"
	if err != nil {
		status = "error"
	}
	s.log.Info("molecule", "status", status, "bytes", len(stl), "ms", time.Since(started).Milliseconds())
	if err != nil {
		switch {
		case errors.Is(err, molecule.ErrFormula):
			writeErr(w, http.StatusBadRequest, "rejected")
		default:
			writeModelErr(w, err)
		}
		return
	}
	w.Header().Set("Content-Type", "model/stl")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="molecule.stl"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(stl)
}

func writeModelErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, model.ErrScript):
		writeErr(w, http.StatusBadRequest, "rejected")
	case errors.Is(err, model.ErrCompile):
		writeErr(w, http.StatusUnprocessableEntity, "model rejected")
	case errors.Is(err, model.ErrTooLarge):
		writeErr(w, http.StatusUnprocessableEntity, "result too large")
	default:
		writeErr(w, http.StatusServiceUnavailable, "renderer unavailable")
	}
}

func writeHandoutErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, handout.ErrBrief):
		writeErr(w, http.StatusBadRequest, "rejected")
	case errors.Is(err, handout.ErrCompile):
		writeErr(w, http.StatusUnprocessableEntity, "handout rejected")
	case errors.Is(err, handout.ErrTooLarge):
		writeErr(w, http.StatusUnprocessableEntity, "result too large")
	default:
		writeErr(w, http.StatusServiceUnavailable, "renderer unavailable")
	}
}

func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		writeErr(w, http.StatusServiceUnavailable, "renderer unavailable")
		return
	}
	id, ok := sessionParam(w, r)
	if !ok {
		return
	}
	if err := s.sessions.Delete(id); err != nil {
		writeSessionErr(w, err)
		return
	}
	s.log.Info("session_delete", "session", id, "status", "ok")
	w.WriteHeader(http.StatusNoContent)
}

func emptyBody(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1)
	buf := make([]byte, 1)
	n, err := r.Body.Read(buf)
	if n > 0 || (err != nil && err != io.EOF) {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return false
	}
	return true
}

func sessionParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !sessionID.MatchString(id) {
		writeErr(w, http.StatusNotFound, "session not found")
		return "", false
	}
	return id, true
}

func writeSessionErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, session.ErrBrief):
		writeErr(w, http.StatusBadRequest, "rejected")
	case errors.Is(err, session.ErrNotFound):
		writeErr(w, http.StatusNotFound, "session not found")
	case errors.Is(err, session.ErrBusy):
		writeErr(w, http.StatusTooManyRequests, "too many sessions")
	case errors.Is(err, session.ErrBudget):
		writeErr(w, http.StatusBadRequest, "call budget exhausted")
	case errors.Is(err, session.ErrTool):
		writeErr(w, http.StatusUnprocessableEntity, "tool rejected the call")
	case errors.Is(err, session.ErrPNG):
		writeErr(w, http.StatusUnprocessableEntity, "export did not return a png")
	case errors.Is(err, session.ErrTooLarge):
		writeErr(w, http.StatusUnprocessableEntity, "result too large")
	default:
		writeErr(w, http.StatusServiceUnavailable, "renderer unavailable")
	}
}

func secureHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "DENY")
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
