package molecule

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"edsger.local/figureserver/internal/model"
)

func TestKnownFormulas(t *testing.T) {
	formulas := []string{"H2O", "OH2", "h2o", "CO2", "co2", "CH4", "NH3", "HCl", "ClH", "C6H6", "H2SO4", "CH3OH", "C2H6", "C2H4", "C2H2", "O2", "N2", "H2", "SO2", "H2O2", "HF"}
	for _, formula := range formulas {
		m, ok := lookup(formula)
		if !ok || len(m.atoms) < 2 || len(m.bonds) < 1 {
			t.Fatalf("%s -> %+v ok=%v", formula, m, ok)
		}
		script := scad(m)
		if strings.Contains(script, "include") || strings.Contains(script, "..") {
			t.Fatalf("unsafe script for %s", formula)
		}
	}
}

func TestSameShapeForElementOrder(t *testing.T) {
	a, _ := lookup("H2O")
	b, _ := lookup("OH2")
	if scad(a) != scad(b) {
		t.Fatal("H2O and OH2 differ")
	}
}

func TestRejectsUnknownFormulas(t *testing.T) {
	c := model.Compiler{Bin: "openscad"}
	cases := []string{
		`{}`,
		`{"formula":""}`,
		`{"formula":"NaCl"}`,
		`{"formula":"H2O #"}`,
		`{"formula":"H2O","extra":true}`,
		`{"formula":"C2H6O"}`,
	}
	for _, raw := range cases {
		_, err := Build(context.Background(), []byte(raw), c)
		if err != ErrFormula {
			t.Fatalf("%s -> %v", raw, err)
		}
	}
}

func TestWaterBuilds(t *testing.T) {
	t.Setenv("FIGURE_TOKEN", "test-token-must-be-at-least-32-bytes")
	stl, err := Build(context.Background(), []byte(`{"formula":"H2O"}`), fakeCompiler(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(stl) != 84+50 {
		t.Fatalf("stl %d", len(stl))
	}
}

func fakeCompiler(t *testing.T) model.Compiler {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "openscad")
	script := "#!/bin/sh\n" +
		"if [ -n \"${FIGURE_TOKEN:-}\" ]; then exit 2; fi\n" +
		"out=\nprev=\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$prev\" = \"-o\" ]; then out=$a; fi\n" +
		"  prev=$a\n" +
		"done\n" +
		"python3 -c 'import sys; sys.stdout.buffer.write(b\"\\0\"*80 + (1).to_bytes(4, \"little\") + b\"\\0\"*50)' > \"$out\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return model.Compiler{Bin: path}
}
