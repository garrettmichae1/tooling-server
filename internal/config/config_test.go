package config

import "testing"

func TestFromEnv(t *testing.T) {
	t.Setenv("FIGURE_TOKEN", "test-token-must-be-at-least-32-bytes")
	t.Setenv("FIGURE_BIND", "")
	t.Setenv("VECTORCRAFT_BIN", "")
	t.Setenv("FIGURE_MAX_SESSIONS", "")
	t.Setenv("TYPST_BIN", "")
	t.Setenv("OPENSCAD_BIN", "")
	t.Setenv("FIGURE_TOOLS_ONLY", "")
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

func TestToolsOnlyAndTokenLimits(t *testing.T) {
	t.Setenv("FIGURE_TOKEN", "test-token-must-be-at-least-32-bytes")
	t.Setenv("FIGURE_BIND", "")
	t.Setenv("FIGURE_TOOLS_ONLY", "1")
	cfg, err := FromEnv()
	if err != nil || !cfg.ToolsOnly {
		t.Fatal(cfg, err)
	}
	t.Setenv("FIGURE_TOOLS_ONLY", "yes")
	if _, err := FromEnv(); err == nil {
		t.Fatal("invalid tools-only setting accepted")
	}
	t.Setenv("FIGURE_TOOLS_ONLY", "")
	t.Setenv("FIGURE_TOKEN", "test token must be at least 32 bytes")
	if _, err := FromEnv(); err == nil {
		t.Fatal("token with spaces accepted")
	}
	for _, bind := range []string{"127.0.0.1:99999", "127.0.0.1:abc", "127.0.0.1:-1", "127.0.0.1:0"} {
		if err := ValidateBind(bind); err == nil {
			t.Fatal("bad bind accepted", bind)
		}
	}
}
