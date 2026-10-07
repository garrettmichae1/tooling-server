// Package artifact reads bounded renderer outputs without loading an oversized
// file into memory first. It rejects symlinks and non-regular files.
package artifact

import (
	"errors"
	"io"
	"os"
)

var ErrTooLarge = errors.New("artifact exceeds size limit")
var ErrFile = errors.New("invalid artifact file")

func Read(path string, max int) ([]byte, error) {
	if max < 1 {
		return nil, ErrFile
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrFile
	}
	if info.Size() > int64(max) {
		return nil, ErrTooLarge
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > max {
		return nil, ErrTooLarge
	}
	return data, nil
}
