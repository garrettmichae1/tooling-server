package config

import "testing"

func TestFromEnv(t *testing.T) {
	t.Setenv("FIGURE_TOKEN", "test-token-must-be-at-least-32-bytes")
	t.Setenv("FIGURE_BIND", "")
	t.Setenv("VECTORCRAFT_BIN", "")
	t.Setenv("FIGURE_MAX_SESSIONS", "")
	t.Setenv("TYPST_BIN", "")
	t.Setenv("OPENSCAD_BIN", "")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bind != DefaultBind {
		t.Fatalf("bind %s", cfg.Bind)
	}

	t.Setenv("FIGURE_TOKEN", "too-short")
	if _, err := FromEnv(); err == nil {
		t.Fatal("short token accepted")
	}

	t.Setenv("FIGURE_TOKEN", "test-token-must-be-at-least-32-bytes")
	t.Setenv("FIGURE_BIND", "0.0.0.0:8787")
	if _, err := FromEnv(); err == nil {
		t.Fatal("public bind accepted")
	}
	t.Setenv("FIGURE_BIND", "localhost:8787")
	if _, err := FromEnv(); err == nil {
		t.Fatal("hostname bind accepted")
	}
}
