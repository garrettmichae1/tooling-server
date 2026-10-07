package session_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"edsger.local/figureserver/internal/fakemcp"
	"edsger.local/figureserver/internal/session"
)

func TestMCPHelper(t *testing.T) {
	if os.Getenv("FIGURE_FAKE_MCP") != "1" {
		return
	}
	fakemcp.Run()
	os.Exit(0)
}

func TestPendingStartupReservesCapacity(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	mgr, err := session.NewManager(session.Config{NewCommand: func() *exec.Cmd { close(entered); <-release; return fakeCommand() }, MaxSessions: 1, MaxCalls: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)
	result := make(chan error, 1)
	go func() { _, err := mgr.Create(context.Background()); result <- err }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("startup did not enter")
	}
	_, secondErr := mgr.Create(context.Background())
	close(release)
	if secondErr != session.ErrBusy {
		t.Fatalf("pending startup did not reserve capacity: %v", secondErr)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func fakeCommand() *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestMCPHelper$")
	cmd.Env = []string{
		"FIGURE_FAKE_MCP=1",
		"PATH=" + os.Getenv("PATH"),
		"TMPDIR=" + os.TempDir(),
	}
	return cmd
}

func TestSessionDrawAndExport(t *testing.T) {
	t.Setenv("FIGURE_TOKEN", "test-token-must-be-at-least-32-bytes")
	mgr, err := session.NewManager(session.Config{
		NewCommand:  fakeCommand,
		MaxSessions: 2,
		MaxCalls:    40,
		MaxPNG:      1024 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)

	sess, err := mgr.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := mgr.Call(context.Background(), sess.ID(), "draw_shape", []byte(`{"shape":"rectangle","x":0,"y":0,"width":10,"height":10,"fill":"#fff"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "leaked") {
		t.Fatal("renderer saw FIGURE_TOKEN")
	}
	png, err := mgr.Export(context.Background(), sess.ID())
	if err != nil {
		t.Fatal(err)
	}
	if len(png) < 8 || png[0] != 0x89 || string(png[1:4]) != "PNG" {
		t.Fatalf("not a png (%d bytes)", len(png))
	}
	if _, err := mgr.Export(context.Background(), sess.ID()); err == nil {
		t.Fatal("export left the session alive")
	}
}

func TestDeleteAndBudget(t *testing.T) {
	mgr, err := session.NewManager(session.Config{
		NewCommand:  fakeCommand,
		MaxSessions: 1,
		MaxCalls:    1,
		MaxPNG:      1024 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)

	sess, err := mgr.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Call(context.Background(), sess.ID(), "undo", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Call(context.Background(), sess.ID(), "redo", []byte(`{}`)); err != session.ErrBudget {
		t.Fatalf("budget: %v", err)
	}
	if err := mgr.Delete(sess.ID()); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Delete(sess.ID()); err != session.ErrNotFound {
		t.Fatalf("second delete: %v", err)
	}
	if _, err := mgr.Create(context.Background()); err != nil {
		t.Fatal(err)
	}
}
