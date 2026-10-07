//go:build integration

package handout

import (
	"context"
	"os"
	"testing"
)

func TestRealSubjects(t *testing.T) {
	bin := os.Getenv("TYPST_BIN")
	if bin == "" {
		t.Skip("TYPST_BIN is not set")
	}
	c := Compiler{Bin: bin, MaxPDF: 8 * 1024 * 1024}
	cases := []string{
		`{"title":"The quadratic formula","kicker":"Algebra","subtitle":"One page for lab","sections":[{"heading":"The formula","body":[{"type":"paragraph","parts":[{"text":"For "},{"math":"ax^2 + bx + c = 0"},{"text":", the solutions are"}]},{"type":"math","latex":"x = \\frac{-b \\pm \\sqrt{b^2 - 4ac}}{2a}"},{"type":"note","label":"Remember","parts":[{"text":"A negative discriminant means no real roots."}]}]}]}`,
		`{"title":"Sulfuric acid","kicker":"Chemistry","sections":[{"heading":"The molecule","body":[{"type":"paragraph","parts":[{"text":"Concentrated sulfuric acid is "},{"math":"\\mathrm{H_2SO_4}"},{"text":"."}]},{"type":"math","latex":"\\mathrm{H_2SO_4}"}]}]}`,
		`{"title":"A running total","kicker":"Python","sections":[{"heading":"The loop","body":[{"type":"code","lang":"python","text":"total = 0\nfor n in (1, 2, 3):\n    total = total + n\n"},{"type":"terms","items":[{"name":"accumulator","parts":[{"text":"A variable that collects a result across a loop."}]}]}]}]}`,
	}
	for _, raw := range cases {
		pdf, err := c.Compile(context.Background(), []byte(raw))
		if err != nil {
			t.Fatalf("compile %s: %v", raw, err)
		}
		if len(pdf) < 1000 || string(pdf[:5]) != "%PDF-" {
			t.Fatalf("pdf %d bytes", len(pdf))
		}
	}
}

func TestReactionAndTriangle(t *testing.T) {
	if os.Getenv("TYPST_BIN") == "" || os.Getenv("OPENSCAD_BIN") == "" {
		t.Skip("TYPST_BIN and OPENSCAD_BIN are not both set")
	}
	c := Compiler{Bin: os.Getenv("TYPST_BIN"), MaxPDF: 8 * 1024 * 1024}
	cases := []string{
		`{"title":"Water","kicker":"Chemistry","sections":[{"heading":"The reaction","body":[{"type":"paragraph","parts":[{"text":"Water is "},{"chem":"H2O"},{"text":"."}]},{"type":"reaction","formula":"HCl + H2O -> H3O+ + Cl-"}]}]}`,
		`{"title":"A right triangle","kicker":"Geometry","sections":[{"heading":"The sides","body":[{"type":"figure","shapes":[{"kind":"polygon","points":[[0,0],[4,0],[0,3]]},{"kind":"label","at":[2,-0.6],"text":"4 cm"},{"kind":"label","at":[-0.8,1.5],"text":"3 cm"},{"kind":"label","at":[2.2,1.6],"text":"5 cm"}]}]}]}`,
	}
	for _, raw := range cases {
		pdf, err := c.Compile(context.Background(), []byte(raw))
		if err != nil {
			t.Fatalf("compile %s: %v", raw, err)
		}
		if len(pdf) < 1000 || string(pdf[:5]) != "%PDF-" {
			t.Fatalf("pdf %d bytes", len(pdf))
		}
	}
}
