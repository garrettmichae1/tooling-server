// Package handout turns a structured page into a PDF.
package handout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const compileTimeout = 20 * time.Second

const (
	maxTitle    = 160
	maxKicker   = 40
	maxSubtitle = 200
	maxHeading  = 120
	maxPart     = 2000
	maxMath     = 2000
	maxCode     = 4000
	maxLabel    = 40
	maxName     = 80
	maxSections = 8
	maxBlocks   = 40
	maxParts    = 24
	maxItems    = 16
	maxTerms    = 12
	maxShapes   = 24
	maxPoints   = 16
)

// ErrBrief means the handout arguments are not usable.
var ErrBrief = errors.New("invalid brief")

// ErrCompile means Typst refused the page.
var ErrCompile = errors.New("handout rejected")

// ErrUnavailable means the typst binary could not be started.
var ErrUnavailable = errors.New("typst unavailable")

// ErrTooLarge means the PDF exceeded the size cap.
var ErrTooLarge = errors.New("result too large")

// Compiler runs a pinned typst binary. Package and font directories sit next to Bin.
type Compiler struct {
	Bin    string
	MaxPDF int
}

// Compile validates the brief, typesets it, and returns a PDF.
func (c Compiler) Compile(ctx context.Context, raw json.RawMessage) ([]byte, error) {
	if c.Bin == "" {
		return nil, ErrUnavailable
	}
	page, err := parse(raw)
	if err != nil {
		return nil, ErrBrief
	}
	dir, err := os.MkdirTemp("", "edsger-handout")
	if err != nil {
		return nil, ErrUnavailable
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, ErrUnavailable
	}
	brief, err := json.Marshal(page)
	if err != nil {
		return nil, ErrBrief
	}
	if err := os.WriteFile(filepath.Join(dir, "brief.json"), brief, 0o600); err != nil {
		return nil, ErrUnavailable
	}
	if err := os.WriteFile(filepath.Join(dir, "handout.typ"), []byte(handoutTemplate), 0o600); err != nil {
		return nil, ErrUnavailable
	}

	root := filepath.Dir(c.Bin)
	ctx, cancel := context.WithTimeout(ctx, compileTimeout)
	defer cancel()
	out := filepath.Join(dir, "handout.pdf")
	cmd := exec.CommandContext(ctx, c.Bin, "compile",
		"--root", dir,
		"--ignore-system-fonts",
		"--font-path", filepath.Join(root, "fonts"),
		"--package-path", filepath.Join(root, "packages"),
		"--package-cache-path", filepath.Join(root, "packages"),
		"--creation-timestamp", "0",
		filepath.Join(dir, "handout.typ"),
		out,
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
	pdf, err := os.ReadFile(out)
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		return nil, ErrCompile
	}
	max := c.MaxPDF
	if max <= 0 {
		max = 8 * 1024 * 1024
	}
	if len(pdf) > max {
		return nil, ErrTooLarge
	}
	return pdf, nil
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

type part struct {
	Text string `json:"text,omitempty"`
	Math string `json:"math,omitempty"`
	Chem string `json:"chem,omitempty"`
}

type shape struct {
	Kind   string      `json:"kind"`
	From   []float64   `json:"from,omitempty"`
	To     []float64   `json:"to,omitempty"`
	At     []float64   `json:"at,omitempty"`
	Radius float64     `json:"radius,omitempty"`
	Width  float64     `json:"width,omitempty"`
	Height float64     `json:"height,omitempty"`
	Points [][]float64 `json:"points,omitempty"`
	Text   string      `json:"text,omitempty"`
}

type term struct {
	Name  string `json:"name"`
	Parts []part `json:"parts"`
}

type block struct {
	Type    string  `json:"type"`
	Parts   []part  `json:"parts,omitempty"`
	Latex   string  `json:"latex,omitempty"`
	Ordered bool    `json:"ordered,omitempty"`
	Items   any     `json:"items,omitempty"`
	Lang    string  `json:"lang,omitempty"`
	Text    string  `json:"text,omitempty"`
	Label   string  `json:"label,omitempty"`
	Formula string  `json:"formula,omitempty"`
	Shapes  []shape `json:"shapes,omitempty"`
}

type section struct {
	Heading string  `json:"heading,omitempty"`
	Body    []block `json:"body"`
}

type page struct {
	Kicker   string    `json:"kicker"`
	Title    string    `json:"title"`
	Subtitle string    `json:"subtitle"`
	Sections []section `json:"sections"`
}

func parse(raw json.RawMessage) (page, error) {
	if len(raw) == 0 {
		return page{}, ErrBrief
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var in struct {
		Kicker   string `json:"kicker"`
		Title    string `json:"title"`
		Subtitle string `json:"subtitle"`
		Sections []struct {
			Heading string            `json:"heading"`
			Body    []json.RawMessage `json:"body"`
		} `json:"sections"`
	}
	if err := dec.Decode(&in); err != nil {
		return page{}, ErrBrief
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return page{}, ErrBrief
	}
	title := strings.TrimSpace(in.Title)
	if title == "" || !short(title, maxTitle) || !plain(title) {
		return page{}, ErrBrief
	}
	kicker := strings.TrimSpace(in.Kicker)
	subtitle := strings.TrimSpace(in.Subtitle)
	if !short(kicker, maxKicker) || !plain(kicker) || !short(subtitle, maxSubtitle) || !plain(subtitle) {
		return page{}, ErrBrief
	}
	if len(in.Sections) == 0 || len(in.Sections) > maxSections {
		return page{}, ErrBrief
	}
	out := page{Kicker: kicker, Title: title, Subtitle: subtitle}
	blocks := 0
	for _, sec := range in.Sections {
		heading := strings.TrimSpace(sec.Heading)
		if !short(heading, maxHeading) || !plain(heading) || len(sec.Body) == 0 {
			return page{}, ErrBrief
		}
		built := section{Heading: heading}
		for _, rawBlock := range sec.Body {
			blocks++
			if blocks > maxBlocks {
				return page{}, ErrBrief
			}
			node, err := parseBlock(rawBlock)
			if err != nil {
				return page{}, err
			}
			built.Body = append(built.Body, node)
		}
		out.Sections = append(out.Sections, built)
	}
	return out, nil
}

func parseBlock(raw json.RawMessage) (block, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var kind struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &kind); err != nil || kind.Type == "" {
		return block{}, ErrBrief
	}
	switch kind.Type {
	case "paragraph":
		var in struct {
			Type  string `json:"type"`
			Parts []part `json:"parts"`
		}
		if err := decode(raw, &in); err != nil || len(in.Parts) == 0 || len(in.Parts) > maxParts {
			return block{}, ErrBrief
		}
		parts, err := cleanParts(in.Parts)
		if err != nil {
			return block{}, err
		}
		return block{Type: "paragraph", Parts: parts}, nil
	case "math":
		var in struct {
			Type  string `json:"type"`
			Latex string `json:"latex"`
		}
		if err := decode(raw, &in); err != nil {
			return block{}, ErrBrief
		}
		latex := strings.TrimSpace(in.Latex)
		if latex == "" || !mathOK(latex) {
			return block{}, ErrBrief
		}
		return block{Type: "math", Latex: latex}, nil
	case "list":
		var in struct {
			Type    string   `json:"type"`
			Ordered bool     `json:"ordered"`
			Items   [][]part `json:"items"`
		}
		if err := decode(raw, &in); err != nil || len(in.Items) == 0 || len(in.Items) > maxItems {
			return block{}, ErrBrief
		}
		items := make([]any, 0, len(in.Items))
		for _, item := range in.Items {
			if len(item) == 0 || len(item) > maxParts {
				return block{}, ErrBrief
			}
			parts, err := cleanParts(item)
			if err != nil {
				return block{}, err
			}
			items = append(items, parts)
		}
		return block{Type: "list", Ordered: in.Ordered, Items: items}, nil
	case "code":
		var in struct {
			Type string `json:"type"`
			Lang string `json:"lang"`
			Text string `json:"text"`
		}
		if err := decode(raw, &in); err != nil {
			return block{}, ErrBrief
		}
		lang := strings.TrimSpace(in.Lang)
		switch lang {
		case "c", "python", "js", "lua":
		default:
			return block{}, ErrBrief
		}
		if in.Text == "" || !short(in.Text, maxCode) || !plain(in.Text) {
			return block{}, ErrBrief
		}
		return block{Type: "code", Lang: lang, Text: in.Text}, nil
	case "terms":
		var in struct {
			Type  string `json:"type"`
			Items []term `json:"items"`
		}
		if err := decode(raw, &in); err != nil || len(in.Items) == 0 || len(in.Items) > maxTerms {
			return block{}, ErrBrief
		}
		items := make([]term, 0, len(in.Items))
		for _, item := range in.Items {
			name := strings.TrimSpace(item.Name)
			if name == "" || !short(name, maxName) || !plain(name) || len(item.Parts) == 0 || len(item.Parts) > maxParts {
				return block{}, ErrBrief
			}
			parts, err := cleanParts(item.Parts)
			if err != nil {
				return block{}, err
			}
			items = append(items, term{Name: name, Parts: parts})
		}
		return block{Type: "terms", Items: items}, nil
	case "note":
		var in struct {
			Type  string `json:"type"`
			Label string `json:"label"`
			Parts []part `json:"parts"`
		}
		if err := decode(raw, &in); err != nil {
			return block{}, ErrBrief
		}
		label := strings.TrimSpace(in.Label)
		if label == "" || !short(label, maxLabel) || !plain(label) || len(in.Parts) == 0 || len(in.Parts) > maxParts {
			return block{}, ErrBrief
		}
		parts, err := cleanParts(in.Parts)
		if err != nil {
			return block{}, err
		}
		return block{Type: "note", Label: label, Parts: parts}, nil
	case "reaction":
		var in struct {
			Type    string `json:"type"`
			Formula string `json:"formula"`
		}
		if err := decode(raw, &in); err != nil {
			return block{}, ErrBrief
		}
		formula := strings.TrimSpace(in.Formula)
		if formula == "" || !chemOK(formula) {
			return block{}, ErrBrief
		}
		return block{Type: "reaction", Formula: formula}, nil
	case "figure":
		var in struct {
			Type   string            `json:"type"`
			Shapes []json.RawMessage `json:"shapes"`
		}
		if err := decode(raw, &in); err != nil || len(in.Shapes) == 0 || len(in.Shapes) > maxShapes {
			return block{}, ErrBrief
		}
		shapes := make([]shape, 0, len(in.Shapes))
		for _, rawShape := range in.Shapes {
			drawn, err := parseShape(rawShape)
			if err != nil {
				return block{}, err
			}
			shapes = append(shapes, drawn)
		}
		return block{Type: "figure", Shapes: shapes}, nil
	default:
		return block{}, ErrBrief
	}
}

func decode(raw json.RawMessage, dest any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("trailing")
	}
	return nil
}

func cleanParts(in []part) ([]part, error) {
	out := make([]part, 0, len(in))
	for _, p := range in {
		text := p.Text
		math := strings.TrimSpace(p.Math)
		chem := strings.TrimSpace(p.Chem)
		filled := 0
		if text != "" {
			filled++
		}
		if math != "" {
			filled++
		}
		if chem != "" {
			filled++
		}
		if filled != 1 {
			return nil, ErrBrief
		}
		switch {
		case chem != "":
			if !chemOK(chem) {
				return nil, ErrBrief
			}
			out = append(out, part{Chem: chem})
		case math != "":
			if !mathOK(math) {
				return nil, ErrBrief
			}
			out = append(out, part{Math: math})
		default:
			if !short(text, maxPart) || !plain(text) {
				return nil, ErrBrief
			}
			out = append(out, part{Text: text})
		}
	}
	return out, nil
}

func parseShape(raw json.RawMessage) (shape, error) {
	var in shape
	if err := decode(raw, &in); err != nil {
		return shape{}, ErrBrief
	}
	switch in.Kind {
	case "line":
		if !pointOK(in.From) || !pointOK(in.To) {
			return shape{}, ErrBrief
		}
		return shape{Kind: "line", From: in.From, To: in.To}, nil
	case "circle":
		if !pointOK(in.At) || !spanOK(in.Radius) {
			return shape{}, ErrBrief
		}
		return shape{Kind: "circle", At: in.At, Radius: in.Radius}, nil
	case "rect":
		if !pointOK(in.At) || !spanOK(in.Width) || !spanOK(in.Height) {
			return shape{}, ErrBrief
		}
		if !coordOK(in.At[0]+in.Width) || !coordOK(in.At[1]+in.Height) {
			return shape{}, ErrBrief
		}
		return shape{Kind: "rect", At: in.At, Width: in.Width, Height: in.Height}, nil
	case "polygon":
		if len(in.Points) < 3 || len(in.Points) > maxPoints {
			return shape{}, ErrBrief
		}
		points := make([][]float64, len(in.Points))
		for i, p := range in.Points {
			if !pointOK(p) {
				return shape{}, ErrBrief
			}
			points[i] = p
		}
		return shape{Kind: "polygon", Points: points}, nil
	case "label":
		if !pointOK(in.At) || in.Text == "" || !short(in.Text, maxPart) || !plain(in.Text) {
			return shape{}, ErrBrief
		}
		return shape{Kind: "label", At: in.At, Text: in.Text}, nil
	default:
		return shape{}, ErrBrief
	}
}

func pointOK(p []float64) bool {
	return len(p) == 2 && coordOK(p[0]) && coordOK(p[1])
}

func coordOK(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= -20 && v <= 20
}

func spanOK(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0 && v <= 40
}

func plain(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0) && short(s, maxCode)
}

func short(s string, max int) bool {
	return len(s) <= max
}

func mathOK(s string) bool {
	if !utf8.ValidString(s) || len(s) > maxMath || strings.ContainsRune(s, 0) {
		return false
	}
	if strings.ContainsAny(s, "#@`") {
		return false
	}
	low := strings.ToLower(s)
	for _, bad := range []string{`\input`, `\include`, `\write`, `\openout`, `\openin`, `\read`} {
		if strings.Contains(low, bad) {
			return false
		}
	}
	return true
}

func chemOK(s string) bool {
	if !utf8.ValidString(s) || len(s) > maxMath || strings.ContainsRune(s, 0) {
		return false
	}
	if strings.ContainsAny(s, "#`\\") || strings.Contains(s, "..") {
		return false
	}
	return true
}
