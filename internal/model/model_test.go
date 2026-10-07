package model

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRejectsUnsafeScripts(t *testing.T) {
	c := Compiler{Bin: "openscad"}
	cases := []string{
		`{}`,
		`{"script":""}`,
		`{"script":"cube(20);","extra":true}`,
		`{"script":"include <other.scad> cube(20);"}`,
		`{"script":"use <other.scad> cube(20);"}`,
		`{"script":"import(\"part.stl\");"}`,
		`{"script":"surface(\"height.png\");"}`,
		`{"script":"cube(20); // ../secret"}`,
		`{"script":"cube(20); // ` + "`" + `"}`,
		`{"script":"import(\"/etc/passwd\");"}`,
		`{"script":"cube(20); // /Users/secret.scad"}`,
		`{"script":"` + strings.Repeat("cube(1);", 4000) + `"}`,
	}
	for _, raw := range cases {
		_, err := c.Compile(context.Background(), []byte(raw))
		if !errors.Is(err, ErrScript) {
			t.Fatalf("script %s -> %v", raw, err)
		}
	}
}

func TestCubeScript(t *testing.T) {
	t.Setenv("FIGURE_TOKEN", "test-token-must-be-at-least-32-bytes")
	c := fakeCompiler(t)
	stl, err := c.Compile(context.Background(), []byte(`{"script":"difference() {\n  cube(20);\n  translate([5, 5, -1]) cylinder(h=22, r=6);\n}"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !stlOK(stl) {
		t.Fatalf("stl %d bytes", len(stl))
	}
	n := binary.LittleEndian.Uint32(stl[80:84])
	if n != 1 {
		t.Fatalf("triangles %d", n)
	}
}

func fakeCompiler(t *testing.T) Compiler {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "openscad")
	script := "#!/bin/sh\n" +
		"if [ -n \"${FIGURE_TOKEN:-}\" ]; then\n" +
		"  exit 2\n" +
		"fi\n" +
		"out=\n" +
		"prev=\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$prev\" = \"-o\" ]; then out=$a; fi\n" +
		"  prev=$a\n" +
		"done\n" +
		"python3 -c 'import sys; sys.stdout.buffer.write(b\"\\0\"*80 + (1).to_bytes(4, \"little\") + b\"\\0\"*50)' > \"$out\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return Compiler{Bin: path}
}
