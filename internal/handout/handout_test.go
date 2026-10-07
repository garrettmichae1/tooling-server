package handout

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
		`{"title":"Pointers"}`,
		`{"title":"Pointers","sections":[]}`,
		`{"title":"Pointers","sections":[{"body":[{"type":"math","latex":"a # b"}]}]}`,
		`{"title":"Pointers","sections":[{"body":[{"type":"math","latex":"\\input{secret}"}]}]}`,
		`{"title":"Pointers","sections":[{"body":[{"type":"math","latex":"\\include{secret}"}]}]}`,
		`{"title":"Pointers","sections":[{"body":[{"type":"paragraph","parts":[{"math":"x @ y"}]}]}]}`,
		`{"title":"Pointers","extra":true,"sections":[{"body":[{"type":"paragraph","parts":[{"text":"Hi"}]}]}]}`,
		`{"title":"Water","sections":[{"body":[{"type":"paragraph","parts":[{"chem":"H2O #"}]}]}]}`,
		`{"title":"Water","sections":[{"body":[{"type":"reaction","formula":"H2O .. O2"}]}]}`,
		`{"title":"Water","sections":[{"body":[{"type":"figure","shapes":[{"kind":"line","from":[0,0],"to":[30,0]}]}]}]}`,
		`{"title":"Water","sections":[{"body":[{"type":"figure","shapes":[{"kind":"sketch"}]}]}]}`,
	}
	for _, raw := range cases {
		_, err := c.Compile(context.Background(), []byte(raw))
		if !errors.Is(err, ErrBrief) {
			t.Fatalf("brief %s -> %v", raw, err)
		}
	}
}

func TestCodeMayContainHash(t *testing.T) {
	t.Setenv("FIGURE_TOKEN", "test-token-must-be-at-least-32-bytes")
	c := fakeCompiler(t)
	raw := []byte(`{"title":"Pointers","kicker":"C","sections":[{"heading":"Store an address","body":[{"type":"code","lang":"c","text":"#include <stdio.h>\nint main(void) { return 0; }\n"},{"type":"paragraph","parts":[{"text":"The address of x is "},{"math":"&x"}]}]}]}`)
	pdf, err := c.Compile(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(pdf) < 5 || string(pdf[:5]) != "%PDF-" {
		t.Fatalf("pdf %q", pdf)
	}
}

func TestChemAndTriangle(t *testing.T) {
	t.Setenv("FIGURE_TOKEN", "test-token-must-be-at-least-32-bytes")
	c := fakeCompiler(t)
	cases := []string{
		`{"title":"Water","kicker":"Chemistry","sections":[{"body":[{"type":"paragraph","parts":[{"text":"Water is "},{"chem":"H2O"},{"text":"."}]},{"type":"reaction","formula":"HCl + H2O -> H3O+ + Cl-"}]}]}`,
		`{"title":"A right triangle","kicker":"Geometry","sections":[{"body":[{"type":"figure","shapes":[{"kind":"polygon","points":[[0,0],[4,0],[0,3]]},{"kind":"label","at":[2,-0.6],"text":"4 cm"},{"kind":"label","at":[-0.8,1.5],"text":"3 cm"},{"kind":"label","at":[2.2,1.6],"text":"5 cm"}]}]}]}`,
	}
	for _, raw := range cases {
		pdf, err := c.Compile(context.Background(), []byte(raw))
		if err != nil {
			t.Fatalf("compile %s: %v", raw, err)
		}
		if len(pdf) < 5 || string(pdf[:5]) != "%PDF-" {
			t.Fatalf("pdf %q", pdf)
		}
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
