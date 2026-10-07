// Package session runs one headless VectorCraft process per figure.
package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"edsger.local/figureserver/internal/compose"
	"edsger.local/figureserver/internal/mcpclient"
)

// ErrBrief means a compose_poster brief was rejected.
var ErrBrief = compose.ErrBrief

const callTimeout = 20 * time.Second

var (
	// ErrNotFound means the session id is unknown or already destroyed.
	ErrNotFound = errors.New("session not found")
	// ErrBusy means the server is already at its session cap.
	ErrBusy = errors.New("too many sessions")
	// ErrBudget means this session has used its call allowance.
	ErrBudget = errors.New("call budget exhausted")
	// ErrTool means VectorCraft rejected the call.
	ErrTool = errors.New("tool rejected the call")
	// ErrPNG means export did not produce a PNG.
	ErrPNG = errors.New("export did not return a png")
	// ErrUnavailable means the renderer could not be started.
	ErrUnavailable = errors.New("renderer unavailable")
	// ErrTooLarge means a tool result exceeded the response cap.
	ErrTooLarge = errors.New("result too large")
)

// Manager caps how many renderers run at once.
type Manager struct {
	newCommand  func() *exec.Cmd
	maxSessions int
	maxCalls    int
	sessionTTL  time.Duration
	idleTTL     time.Duration
	maxPNG      int
	root        string

	mu       sync.Mutex
	sessions map[string]*Session
	closed   bool
}

// Session is one renderer and its private scratch directory.
type Session struct {
	id       string
	dir      string
	cmd      *exec.Cmd
	client   *mcpclient.Client
	calls    int
	maxCalls int
	expires  time.Time
	idle     time.Duration
	maxPNG   int
	touchCh  chan struct{}
	done     chan struct{}
	once     sync.Once
	mu       sync.Mutex
	inFlight int
	forget   func(string)
}

// Config builds a Manager. NewCommand must return an unstarted command.
type Config struct {
	NewCommand  func() *exec.Cmd
	MaxSessions int
	MaxCalls    int
	SessionTTL  time.Duration
	IdleTTL     time.Duration
	MaxPNG      int
	ScratchRoot string
}

// NewManager stores sessions under ScratchRoot, or a private temp directory.
func NewManager(cfg Config) (*Manager, error) {
	if cfg.NewCommand == nil {
		return nil, errors.New("missing command")
	}
	if cfg.MaxSessions < 1 {
		cfg.MaxSessions = 1
	}
	if cfg.MaxCalls < 1 {
		cfg.MaxCalls = 1
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 90 * time.Second
	}
	if cfg.IdleTTL <= 0 {
		cfg.IdleTTL = 30 * time.Second
	}
	if cfg.MaxPNG <= 0 {
		cfg.MaxPNG = 8 * 1024 * 1024
	}
	root := cfg.ScratchRoot
	if root == "" {
		root = filepath.Join(os.TempDir(), "edsger-figures")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &Manager{
		newCommand:  cfg.NewCommand,
		maxSessions: cfg.MaxSessions,
		maxCalls:    cfg.MaxCalls,
		sessionTTL:  cfg.SessionTTL,
		idleTTL:     cfg.IdleTTL,
		maxPNG:      cfg.MaxPNG,
		root:        root,
		sessions:    map[string]*Session{},
	}, nil
}

// Create starts a renderer. The scratch directory is removed when the session ends.
func (m *Manager) Create(ctx context.Context) (*Session, error) {
	m.mu.Lock()
	if m.closed || len(m.sessions) >= m.maxSessions {
		m.mu.Unlock()
		if m.closed {
			return nil, ErrUnavailable
		}
		return nil, ErrBusy
	}
	m.mu.Unlock()

	id, err := newID()
	if err != nil {
		return nil, ErrUnavailable
	}
	dir, err := os.MkdirTemp(m.root, id+"-")
	if err != nil {
		return nil, ErrUnavailable
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return nil, ErrUnavailable
	}

	cmd := m.newCommand()
	cmd.Dir = dir
	cmd.Env = scopedEnv(cmd.Env, dir)
	setGroup(cmd)

	initCtx, cancel := context.WithTimeout(ctx, callTimeout)
	client, err := mcpclient.Start(initCtx, cmd)
	cancel()
	if err != nil {
		if cmd.Process != nil {
			killGroup(cmd.Process.Pid)
			_, _ = cmd.Process.Wait()
		}
		_ = os.RemoveAll(dir)
		return nil, ErrUnavailable
	}

	now := time.Now()
	s := &Session{
		id:       id,
		dir:      dir,
		cmd:      cmd,
		client:   client,
		maxCalls: m.maxCalls,
		expires:  now.Add(m.sessionTTL),
		idle:     m.idleTTL,
		maxPNG:   m.maxPNG,
		touchCh:  make(chan struct{}, 1),
		done:     make(chan struct{}),
		forget:   m.forget,
	}
	m.mu.Lock()
	if m.closed || len(m.sessions) >= m.maxSessions {
		m.mu.Unlock()
		s.destroy()
		if m.closed {
			return nil, ErrUnavailable
		}
		return nil, ErrBusy
	}
	m.sessions[id] = s
	m.mu.Unlock()
	go s.watch()
	return s, nil
}

// Call forwards one allowlisted tool. The caller has already validated the name and arguments.
func (m *Manager) Call(ctx context.Context, id, tool string, arguments json.RawMessage) (json.RawMessage, error) {
	s, err := m.get(id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	select {
	case <-s.done:
		s.mu.Unlock()
		return nil, ErrNotFound
	default:
	}
	if s.calls >= s.maxCalls {
		s.mu.Unlock()
		return nil, ErrBudget
	}
	s.calls++
	s.inFlight++
	s.touch()
	s.mu.Unlock()

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	var result json.RawMessage
	var callErr error
	if tool == "compose_poster" {
		result, callErr = compose.Poster(callCtx, s.client, arguments)
	} else {
		result, callErr = s.client.Call(callCtx, tool, arguments)
	}
	cancel()

	s.mu.Lock()
	s.inFlight--
	s.touch()
	s.mu.Unlock()
	if callErr != nil {
		if errors.Is(callErr, compose.ErrBrief) {
			return nil, ErrBrief
		}
		if errors.Is(callErr, mcpclient.ErrClosed) || errors.Is(callErr, context.DeadlineExceeded) {
			s.destroy()
		}
		return nil, ErrTool
	}
	if len(result) > 256*1024 {
		return nil, ErrTooLarge
	}
	return scrub(result, s.dir), nil
}

// Export renders a PNG to the scratch directory, returns the bytes, and destroys the session.
func (m *Manager) Export(ctx context.Context, id string) ([]byte, error) {
	s, err := m.get(id)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(s.dir, "figure.png")
	args, _ := json.Marshal(map[string]string{"path": path, "format": "png"})

	s.mu.Lock()
	s.inFlight++
	s.touch()
	s.mu.Unlock()

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	result, _ := s.client.Call(callCtx, "export", args)
	cancel()

	png, readErr := os.ReadFile(path)
	if readErr != nil {
		png = pngFromResult(result, s.maxPNG)
	}
	s.destroy()
	if !validPNG(png) || len(png) > s.maxPNG {
		return nil, ErrPNG
	}
	return png, nil
}

// Delete destroys a session without returning an image.
func (m *Manager) Delete(id string) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}
	s.destroy()
	return nil
}

// Close destroys every session. Used on process shutdown.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.mu.Unlock()
	for _, s := range all {
		s.destroy()
	}
}

func (m *Manager) get(id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}

func (m *Manager) forget(id string) {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}

// ID is the public session identifier.
func (s *Session) ID() string { return s.id }

// Expires is when the session is destroyed even if the caller is still drawing.
func (s *Session) Expires() time.Time { return s.expires }

func (s *Session) touch() {
	select {
	case <-s.done:
	case s.touchCh <- struct{}{}:
	default:
	}
}

func (s *Session) watch() {
	idle := time.NewTimer(s.idle)
	absolute := time.NewTimer(time.Until(s.expires))
	defer idle.Stop()
	defer absolute.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-absolute.C:
			s.destroy()
			return
		case <-idle.C:
			s.mu.Lock()
			busy := s.inFlight > 0
			s.mu.Unlock()
			if busy {
				idle.Reset(s.idle)
				continue
			}
			s.destroy()
			return
		case <-s.touchCh:
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(s.idle)
		}
	}
}

func (s *Session) destroy() {
	s.once.Do(func() {
		select {
		case <-s.done:
		default:
			close(s.done)
		}
		if s.cmd != nil && s.cmd.Process != nil {
			killGroup(s.cmd.Process.Pid)
		}
		if s.client != nil {
			s.client.Close()
		}
		if s.cmd != nil && s.cmd.Process != nil {
			_, _ = s.cmd.Process.Wait()
		}
		if s.dir != "" {
			_ = os.RemoveAll(s.dir)
		}
		if s.forget != nil {
			s.forget(s.id)
		}
	})
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func scopedEnv(existing []string, scratch string) []string {
	env := minimalEnv(scratch)
	for _, entry := range existing {
		if strings.HasPrefix(entry, "FIGURE_FAKE_MCP=") {
			env = append(env, entry)
		}
	}
	return env
}

func minimalEnv(scratch string) []string {
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/bin:/bin"
	}
	return []string{
		"PATH=" + path,
		"HOME=" + scratch,
		"TMPDIR=" + scratch,
		"LANG=C",
	}
}

func validPNG(b []byte) bool {
	return len(b) >= 8 &&
		b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G' &&
		b[4] == '\r' && b[5] == '\n' && b[6] == 0x1a && b[7] == '\n'
}

func pngFromResult(raw json.RawMessage, max int) []byte {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	encoded := findKey(v, "dataBase64")
	if encoded == "" {
		return nil
	}
	if len(encoded) > max*2 {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil
	}
	return decoded
}

func findKey(v any, key string) string {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if k == key {
				if s, ok := child.(string); ok {
					return s
				}
			}
			if s := findKey(child, key); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range t {
			if s := findKey(child, key); s != "" {
				return s
			}
		}
	}
	return ""
}

func scrub(raw json.RawMessage, secret string) json.RawMessage {
	if len(raw) == 0 {
		return []byte(`{}`)
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return []byte(`{}`)
	}
	clean, err := json.Marshal(redact(v, secret))
	if err != nil {
		return []byte(`{}`)
	}
	return clean
}

func redact(v any, secret string) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, child := range t {
			out[k] = redact(child, secret)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, child := range t {
			out[i] = redact(child, secret)
		}
		return out
	case string:
		if secret != "" && strings.Contains(t, secret) {
			return "[redacted]"
		}
		return t
	default:
		return v
	}
}
