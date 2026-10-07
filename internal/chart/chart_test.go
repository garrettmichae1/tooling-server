package chart

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRejectsUnsafeBriefs(t *testing.T) {
	c := Compiler{Bin: "typst"}
	cases := []string{
		`{}`,
		`{"title":"Hours"}`,
		`{"title":"Hours","type":"sketch","series":[1,2]}`,
		`{"title":"../secret","type":"column","categories":["A"],"series":[1]}`,
		`{"title":"Hours","type":"column","xlabel":"/etc/passwd","categories":["A"],"series":[1]}`,
		`{"title":"Hours","type":"column","categories":["A"],"series":[1],"extra":true}`,
		`{"title":"Hours","type":"pie","categories":["A"],"series":[1]}`,
		`{"title":"Hours","type":"pie","categories":["A","B"],"series":[-1,2]}`,
		`{"title":"Hours","type":"column","stacked":true,"categories":["A","B"],"series":[1,2]}`,
		`{"title":"Hours","type":"line","stacked":true,"categories":["A","B"],"series":[1,2]}`,
		`{"title":"Hours","type":"area","categories":["A","B"],"series":[1,-2]}`,
		`{"title":"Hours","type":"column","categories":["A","B"],"series":[{"name":"One","values":[1]}]}`,
		`{"title":"Hours","type":"column","categories":["A"],"series":[{"name":"","values":[1]},{"name":"","values":[2]}]}`,
		`{"title":"Hours","type":"line","series":[{"name":"A","points":[[0,1]]}]}`,
		`{"title":"Hours","type":"column","categories":["A"],"series":[{"name":"A","points":[[0,1]]}]}`,
	}
	for _, raw := range cases {
		_, err := c.Compile(context.Background(), []byte(raw))
		if !errors.Is(err, ErrBrief) {
			t.Fatalf("brief %s -> %v", raw, err)
		}
	}
}

func TestColumnBriefCompiles(t *testing.T) {
	t.Setenv("FIGURE_TOKEN", "test-token-must-be-at-least-32-bytes")
	c := fakeCompiler(t)
	raw := []byte(`{"title":"Study hours","type":"column","xlabel":"Language","ylabel":"Hours","categories":["C","Python","JS","Lua"],"series":[4,7,5,3]}`)
	pdf, err := c.Compile(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(pdf) < 5 || string(pdf[:5]) != "%PDF-" {
		t.Fatalf("pdf %q", pdf)
	}
}

func fakeCompiler(t *testing.T) Compiler {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "typst")
	script := "#!/bin/sh\nif [ -n \"${FIGURE_TOKEN:-}\" ]; then exit 2; fi\nout=\nfor a in \"$@\"; do out=$a; done\nprintf '%s\\n' '%PDF-1.4' > \"$out\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return Compiler{Bin: path, MaxPDF: 1024}
}
