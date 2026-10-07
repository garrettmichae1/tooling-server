//go:build integration

package molecule

import (
	"context"
	"os"
	"testing"

	"edsger.local/figureserver/internal/model"
)

func TestRealWater(t *testing.T) {
	bin := os.Getenv("OPENSCAD_BIN")
	if bin == "" {
		t.Skip("OPENSCAD_BIN is not set")
	}
	stl, err := Build(context.Background(), []byte(`{"formula":"H2O"}`), model.Compiler{Bin: bin})
	if err != nil {
		t.Fatal(err)
	}
	if len(stl) < 84+50 {
		t.Fatalf("stl %d bytes", len(stl))
	}
}
