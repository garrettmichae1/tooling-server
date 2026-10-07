// Package config loads the figure server settings from the environment.
package config

import (
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	DefaultBind          = "127.0.0.1:8787"
	DefaultBin           = "third_party/vectorcraft/target/release/vectorcraft-cli"
	DefaultTypst         = "third_party/typst/typst"
	DefaultOpenSCAD      = "third_party/openscad/openscad"
	MinTokenBytes        = 32
	DefaultMaxSessions   = 2
	DefaultMaxCalls      = 40
	DefaultMaxBody       = 256 * 1024
	DefaultMaxPNG        = 8 * 1024 * 1024
	DefaultMaxPDF        = 8 * 1024 * 1024
	DefaultRatePerMinute = 30
)

// Config is the process configuration. The bearer token is never taken from flags.
type Config struct {
	Token         string
	Bind          string
	VectorCraft   string
	Typst         string
	OpenSCAD      string
	MaxSessions   int
	MaxCalls      int
	SessionTTL    time.Duration
	IdleTTL       time.Duration
	MaxBody       int64
	MaxPNG        int
	MaxPDF        int
	RatePerMinute int
	ToolsOnly     bool
}

// FromEnv reads configuration. It fails closed when the token or bind address is unsafe.
func FromEnv() (Config, error) {
	token := os.Getenv("FIGURE_TOKEN")
	if len(token) < MinTokenBytes {
		return Config{}, errors.New("FIGURE_TOKEN must be at least 32 bytes")
	}
	if len(token) > 256 || strings.IndexFunc(token, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return Config{}, errors.New("FIGURE_TOKEN must be at most 256 bytes without spaces or control characters")
	}

	bind := os.Getenv("FIGURE_BIND")
	if bind == "" {
		bind = DefaultBind
	}
	if err := ValidateBind(bind); err != nil {
		return Config{}, err
	}

	bin := os.Getenv("VECTORCRAFT_BIN")
	if bin == "" {
		bin = DefaultBin
	}
	typst := os.Getenv("TYPST_BIN")
	if typst == "" {
		typst = DefaultTypst
	}
	openscad := os.Getenv("OPENSCAD_BIN")
	if openscad == "" {
		openscad = DefaultOpenSCAD
	}

	cfg := Config{
		Token:         token,
		Bind:          bind,
		VectorCraft:   bin,
		Typst:         typst,
		OpenSCAD:      openscad,
		MaxSessions:   DefaultMaxSessions,
		MaxCalls:      DefaultMaxCalls,
		SessionTTL:    90 * time.Second,
		IdleTTL:       30 * time.Second,
		MaxBody:       DefaultMaxBody,
		MaxPNG:        DefaultMaxPNG,
		MaxPDF:        DefaultMaxPDF,
		RatePerMinute: DefaultRatePerMinute,
	}
	if v := os.Getenv("FIGURE_MAX_SESSIONS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 2 {
			return Config{}, errors.New("FIGURE_MAX_SESSIONS must be 1 or 2")
		}
		cfg.MaxSessions = n
	}
	if v := os.Getenv("FIGURE_TOOLS_ONLY"); v != "" {
		if v != "0" && v != "1" {
			return Config{}, errors.New("FIGURE_TOOLS_ONLY must be 0 or 1")
		}
		cfg.ToolsOnly = v == "1"
	}
	return cfg, nil
}

// ValidateBind allows only numeric loopback addresses, so the server cannot be published by a typo.
func ValidateBind(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return errors.New("FIGURE_BIND must be host:port")
	}
	n, parseErr := strconv.Atoi(port)
	if parseErr != nil || n < 1 || n > 65535 {
		return errors.New("FIGURE_BIND must use an explicit port")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("FIGURE_BIND must be a loopback address such as 127.0.0.1:8787")
	}
	return nil
}
