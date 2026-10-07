//go:build integration

package model

import (
	"context"
	"os"
	"testing"
)

func TestRealCube(t *testing.T) {
	bin := os.Getenv("OPENSCAD_BIN")
	if bin == "" || os.Getenv("TYPST_BIN") == "" {
		t.Skip("TYPST_BIN and OPENSCAD_BIN are not both set")
	}
	c := Compiler{Bin: bin}
	stl, err := c.Compile(context.Background(), []byte(`{"script":"cube(20);"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !stlOK(stl) || len(stl) < stlHeader+stlTriangle {
		t.Fatalf("stl %d bytes", len(stl))
	}
}
