//go:build integration

package session_test

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"edsger.local/figureserver/internal/session"
)

func TestVectorCraftSmoke(t *testing.T) {
	bin := os.Getenv("VECTORCRAFT_BIN")
	if bin == "" {
		t.Skip("VECTORCRAFT_BIN not set")
	}
	mgr, err := session.NewManager(session.Config{
		NewCommand: func() *exec.Cmd {
			return exec.Command(bin, "mcp", "--headless")
		},
		MaxSessions: 1,
		MaxCalls:    10,
		MaxPNG:      8 * 1024 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)

	sess, err := mgr.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.Call(context.Background(), sess.ID(), "draw_shape", []byte(`{"shape":"rectangle","x":0,"y":0,"width":120,"height":80,"fill":"#112233"}`))
	if err != nil {
		t.Fatal(err)
	}
	png, err := mgr.Export(context.Background(), sess.ID())
	if err != nil {
		t.Fatal(err)
	}
	if len(png) < 8 || png[0] != 0x89 {
		t.Fatalf("not a png (%d bytes)", len(png))
	}
}
