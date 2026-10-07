package artifact

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBoundedRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out")
	if err := os.WriteFile(path, []byte("1234"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := Read(path, 4)
	if err != nil || string(data) != "1234" {
		t.Fatal(data, err)
	}
	if _, err := Read(path, 3); !errors.Is(err, ErrTooLarge) {
		t.Fatal("oversized file accepted", err)
	}
	if _, err := Read(dir, 10); !errors.Is(err, ErrFile) {
		t.Fatal("directory accepted", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(link, 10); !errors.Is(err, ErrFile) {
		t.Fatal("symlink accepted", err)
	}
}
