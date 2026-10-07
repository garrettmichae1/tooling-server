// Package model turns an OpenSCAD script into a binary STL.
package model

import (
	"context"
	"edsger.local/figureserver/internal/artifact"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const compileTimeout = 20 * time.Second

const (
	maxScript    = 16 * 1024
	maxTriangles = 200000
	stlHeader    = 84
	stlTriangle  = 50
)

// ErrScript means the script is not allowed to run.
var ErrScript = errors.New("invalid script")

// ErrCompile means OpenSCAD refused the script or the STL was not usable.
var ErrCompile = errors.New("model rejected")

// ErrUnavailable means the openscad binary could not be started.
var ErrUnavailable = errors.New("openscad unavailable")

// ErrTooLarge means the mesh has more triangles than the cap.
var ErrTooLarge = errors.New("result too large")

// Compiler runs a pinned openscad binary.
type Compiler struct {
	Bin string
}

// Compile validates the script, renders it, and returns a binary STL.
func (c Compiler) Compile(ctx context.Context, raw json.RawMessage) ([]byte, error) {
	if c.Bin == "" {
		return nil, ErrUnavailable
	}
	script, err := parse(raw)
	if err != nil {
		return nil, ErrScript
	}
	return c.Render(ctx, script)
}

// Render runs a server-built OpenSCAD script. Callers that accept an agent
// script must validate it before calling Render.
func (c Compiler) Render(ctx context.Context, script string) ([]byte, error) {
	if c.Bin == "" {
		return nil, ErrUnavailable
	}
	if strings.TrimSpace(script) == "" {
		return nil, ErrScript
	}
	dir, err := os.MkdirTemp("", "edsger-model")
	if err != nil {
		return nil, ErrUnavailable
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, ErrUnavailable
	}
	src := filepath.Join(dir, "model.scad")
	out := filepath.Join(dir, "model.stl")
	if err := os.WriteFile(src, []byte(script), 0o600); err != nil {
		return nil, ErrUnavailable
	}

	ctx, cancel := context.WithTimeout(ctx, compileTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Bin,
		"--export-format", "binstl",
		"--hardwarnings",
		"-o", out,
		src,
	)
	cmd.Dir = dir
	cmd.Env = minimalEnv(dir)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	setGroup(cmd)
	if err := cmd.Start(); err != nil {
		return nil, ErrUnavailable
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	select {
	case <-ctx.Done():
		killGroup(cmd.Process.Pid)
		<-wait
		return nil, ErrUnavailable
	case err := <-wait:
		if err != nil {
			return nil, ErrCompile
		}
	}
	stl, err := artifact.Read(out, stlHeader+maxTriangles*stlTriangle)
	if errors.Is(err, artifact.ErrTooLarge) {
		return nil, ErrTooLarge
	}
	if err != nil {
		return nil, ErrCompile
	}
	if err := checkSTL(stl); err != nil {
		return nil, err
	}
	return stl, nil
}

func parse(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", ErrScript
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var in struct {
		Script string `json:"script"`
	}
	if err := dec.Decode(&in); err != nil {
		return "", ErrScript
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return "", ErrScript
	}
	script := strings.TrimSpace(in.Script)
	if !scriptOK(script) {
		return "", ErrScript
	}
	return script, nil
}

var fileWord = regexp.MustCompile(`(?i)\b(include|use|import|surface)\b`)

func scriptOK(s string) bool {
	if s == "" || len(s) > maxScript || !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
		return false
	}
	if strings.Contains(s, "..") || strings.Contains(s, "`") || fileWord.MatchString(s) {
		return false
	}
	return !hasAbsPath(s)
}

func hasAbsPath(s string) bool {
	if strings.Contains(s, `"/`) || strings.Contains(s, "'/") || strings.Contains(s, "</") {
		return true
	}
	for _, field := range strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case ' ', '\n', '\t', '\r', ';', ',', '(', ')', '{', '}', '[', ']':
			return true
		default:
			return false
		}
	}) {
		if strings.HasPrefix(field, "/") {
			return true
		}
	}
	return false
}

func checkSTL(b []byte) error {
	if len(b) < stlHeader || (len(b)-stlHeader)%stlTriangle != 0 {
		return ErrCompile
	}
	n := binary.LittleEndian.Uint32(b[80:84])
	if int(n) != (len(b)-stlHeader)/stlTriangle || n < 1 {
		return ErrCompile
	}
	if n > maxTriangles {
		return ErrTooLarge
	}
	return nil
}

func stlOK(b []byte) bool {
	return checkSTL(b) == nil
}

func minimalEnv(scratch string) []string {
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/bin:/bin"
	}
	return []string{
		"PATH=" + path,
		"HOME=" + scratch,
		"TMPDIR=" + scratch,
		"LANG=C",
	}
}
