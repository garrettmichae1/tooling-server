// Package chart turns a table of numbers into a PDF chart.
package chart

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
	maxTitle      = 120
	maxLabel      = 80
	maxName       = 40
	maxCategory   = 40
	maxCategories = 12
	maxSeries     = 4
	maxPoints     = 48
	maxAbs        = 1e9
)

// ErrBrief means the chart arguments are not usable.
var ErrBrief = errors.New("invalid brief")

// ErrCompile means Typst refused the chart.
var ErrCompile = errors.New("chart rejected")

// ErrUnavailable means the typst binary could not be started.
var ErrUnavailable = errors.New("typst unavailable")

// ErrTooLarge means the PDF exceeded the size cap.
var ErrTooLarge = errors.New("result too large")

// Compiler runs a pinned typst binary. Package and font directories sit next to Bin.
type Compiler struct {
	Bin    string
	MaxPDF int
}

// Compile validates the brief, draws the chart, and returns a PDF.
func (c Compiler) Compile(ctx context.Context, raw json.RawMessage) ([]byte, error) {
	if c.Bin == "" {
		return nil, ErrUnavailable
	}
	page, err := parse(raw)
	if err != nil {
		return nil, ErrBrief
	}
	dir, err := os.MkdirTemp("", "edsger-chart")
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
	if err := os.WriteFile(filepath.Join(dir, "chart.typ"), []byte(chartTemplate), 0o600); err != nil {
		return nil, ErrUnavailable
	}

	root := filepath.Dir(c.Bin)
	ctx, cancel := context.WithTimeout(ctx, compileTimeout)
	defer cancel()
	out := filepath.Join(dir, "chart.pdf")
	cmd := exec.CommandContext(ctx, c.Bin, "compile",
		"--root", dir,
		"--ignore-system-fonts",
		"--font-path", filepath.Join(root, "fonts"),
		"--package-path", filepath.Join(root, "packages"),
		"--package-cache-path", filepath.Join(root, "packages"),
		"--creation-timestamp", "0",
		filepath.Join(dir, "chart.typ"),
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

type series struct {
	Name   string      `json:"name"`
	Values []float64   `json:"values"`
	Points [][]float64 `json:"points"`
}

type page struct {
	Title      string   `json:"title"`
	Type       string   `json:"type"`
	XLabel     string   `json:"xlabel"`
	YLabel     string   `json:"ylabel"`
	Stacked    bool     `json:"stacked"`
	Categories []string `json:"categories"`
	Series     []series `json:"series"`
	XMin       *float64 `json:"xmin,omitempty"`
	XMax       *float64 `json:"xmax,omitempty"`
	YMin       *float64 `json:"ymin,omitempty"`
	YMax       *float64 `json:"ymax,omitempty"`
}

func parse(raw json.RawMessage) (page, error) {
	if len(raw) == 0 {
		return page{}, ErrBrief
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var in struct {
		Title      string          `json:"title"`
		Type       string          `json:"type"`
		XLabel     string          `json:"xlabel"`
		YLabel     string          `json:"ylabel"`
		Stacked    bool            `json:"stacked"`
		Categories []string        `json:"categories"`
		Series     json.RawMessage `json:"series"`
	}
	if err := dec.Decode(&in); err != nil {
		return page{}, ErrBrief
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return page{}, ErrBrief
	}
	title := strings.TrimSpace(in.Title)
	if title == "" || !labelOK(title, maxTitle) {
		return page{}, ErrBrief
	}
	kind := strings.TrimSpace(in.Type)
	switch kind {
	case "column", "bar", "line", "area", "scatter", "pie":
	default:
		return page{}, ErrBrief
	}
	xlabel := strings.TrimSpace(in.XLabel)
	ylabel := strings.TrimSpace(in.YLabel)
	if !labelOK(xlabel, maxLabel) || !labelOK(ylabel, maxLabel) {
		return page{}, ErrBrief
	}
	if len(in.Categories) > maxCategories {
		return page{}, ErrBrief
	}
	categories := make([]string, len(in.Categories))
	for i, c := range in.Categories {
		c = strings.TrimSpace(c)
		if c == "" || !labelOK(c, maxCategory) {
			return page{}, ErrBrief
		}
		categories[i] = c
	}
	series, points, err := parseSeries(in.Series)
	if err != nil {
		return page{}, err
	}
	out := page{
		Title:      title,
		Type:       kind,
		XLabel:     xlabel,
		YLabel:     ylabel,
		Stacked:    in.Stacked,
		Categories: categories,
		Series:     series,
	}
	if err := checkShape(&out, points); err != nil {
		return page{}, err
	}
	return out, nil
}

func parseSeries(raw json.RawMessage) ([]series, bool, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, false, ErrBrief
	}
	var items []json.RawMessage
	if err := decode(raw, &items); err != nil || len(items) == 0 {
		return nil, false, ErrBrief
	}
	first := bytes.TrimSpace(items[0])
	if len(first) == 0 {
		return nil, false, ErrBrief
	}
	if first[0] != '{' {
		if len(items) > maxCategories {
			return nil, false, ErrBrief
		}
		values := make([]float64, len(items))
		for i, item := range items {
			var n float64
			if err := decode(item, &n); err != nil || !numberOK(n) {
				return nil, false, ErrBrief
			}
			values[i] = n
		}
		return []series{{Values: values, Points: [][]float64{}}}, false, nil
	}
	if len(items) > maxSeries {
		return nil, false, ErrBrief
	}
	out := make([]series, 0, len(items))
	points := false
	for i, item := range items {
		var row struct {
			Name   string      `json:"name"`
			Values []float64   `json:"values"`
			Points [][]float64 `json:"points"`
		}
		if err := decode(item, &row); err != nil {
			return nil, false, ErrBrief
		}
		hasValues := len(row.Values) > 0
		hasPoints := len(row.Points) > 0
		if hasValues == hasPoints {
			return nil, false, ErrBrief
		}
		if i == 0 {
			points = hasPoints
		} else if points != hasPoints {
			return nil, false, ErrBrief
		}
		name := strings.TrimSpace(row.Name)
		if !labelOK(name, maxName) {
			return nil, false, ErrBrief
		}
		if len(items) > 1 && name == "" {
			return nil, false, ErrBrief
		}
		built := series{Name: name, Values: []float64{}, Points: [][]float64{}}
		if hasValues {
			if len(row.Values) > maxCategories {
				return nil, false, ErrBrief
			}
			for _, n := range row.Values {
				if !numberOK(n) {
					return nil, false, ErrBrief
				}
			}
			built.Values = row.Values
		} else {
			if len(row.Points) > maxPoints {
				return nil, false, ErrBrief
			}
			pts := make([][]float64, len(row.Points))
			for j, p := range row.Points {
				if len(p) != 2 || !numberOK(p[0]) || !numberOK(p[1]) {
					return nil, false, ErrBrief
				}
				pts[j] = []float64{p[0], p[1]}
			}
			built.Points = pts
		}
		out = append(out, built)
	}
	return out, points, nil
}

func checkShape(out *page, points bool) error {
	switch out.Type {
	case "column", "bar":
		if points || (out.Stacked && len(out.Series) < 2) {
			return ErrBrief
		}
		if err := valuesMatch(out); err != nil {
			return err
		}
		if out.Stacked && !nonNegative(out) {
			return ErrBrief
		}
	case "pie":
		if points || out.Stacked || len(out.Series) != 1 || len(out.Categories) < 2 {
			return ErrBrief
		}
		if err := valuesMatch(out); err != nil {
			return err
		}
		if !nonNegative(out) || sum(out.Series[0].Values) <= 0 {
			return ErrBrief
		}
	case "line", "area", "scatter":
		if out.Stacked {
			return ErrBrief
		}
		if points {
			if len(out.Categories) > 0 {
				return ErrBrief
			}
			minN := 2
			if out.Type == "scatter" {
				minN = 1
			}
			for i := range out.Series {
				if len(out.Series[i].Points) < minN {
					return ErrBrief
				}
			}
			setBounds(out)
			if out.Type == "area" && !nonNegative(out) {
				return ErrBrief
			}
			if out.Type == "area" {
				zero := 0.0
				out.YMin = &zero
				yMax := out.seriesMaxY() + headroom(0, out.seriesMaxY())
				out.YMax = &yMax
			}
		} else {
			minCats := 2
			if out.Type == "scatter" {
				minCats = 1
			}
			if len(out.Categories) < minCats {
				return ErrBrief
			}
			if err := valuesMatch(out); err != nil {
				return err
			}
			if out.Type == "area" && !nonNegative(out) {
				return ErrBrief
			}
		}
	default:
		return ErrBrief
	}
	return nil
}

func valuesMatch(out *page) error {
	if len(out.Categories) == 0 {
		return ErrBrief
	}
	for _, s := range out.Series {
		if len(s.Values) != len(out.Categories) {
			return ErrBrief
		}
	}
	return nil
}

func nonNegative(out *page) bool {
	for _, s := range out.Series {
		for _, n := range s.Values {
			if n < 0 {
				return false
			}
		}
		for _, p := range s.Points {
			if p[1] < 0 {
				return false
			}
		}
	}
	return true
}

func sum(values []float64) float64 {
	total := 0.0
	for _, n := range values {
		total += n
	}
	return total
}

func setBounds(out *page) {
	xmin, xmax := out.Series[0].Points[0][0], out.Series[0].Points[0][0]
	ymin, ymax := out.Series[0].Points[0][1], out.Series[0].Points[0][1]
	for _, s := range out.Series {
		for _, p := range s.Points {
			xmin, xmax = math.Min(xmin, p[0]), math.Max(xmax, p[0])
			ymin, ymax = math.Min(ymin, p[1]), math.Max(ymax, p[1])
		}
	}
	padX := headroom(xmin, xmax)
	xmin, xmax = xmin-padX, xmax+padX
	padY := headroom(ymin, ymax)
	ymin, ymax = ymin-padY, ymax+padY
	out.XMin, out.XMax = &xmin, &xmax
	out.YMin, out.YMax = &ymin, &ymax
}

func (p page) seriesMaxY() float64 {
	max := 0.0
	for _, s := range p.Series {
		for _, n := range s.Values {
			max = math.Max(max, n)
		}
		for _, pt := range s.Points {
			max = math.Max(max, pt[1])
		}
	}
	return max
}

func headroom(min, max float64) float64 {
	pad := (max - min) * 0.06
	if pad == 0 {
		return 1
	}
	return pad
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

func numberOK(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && math.Abs(v) <= maxAbs
}

func labelOK(s string, max int) bool {
	if !utf8.ValidString(s) || len(s) > max || strings.ContainsRune(s, 0) {
		return false
	}
	if strings.Contains(s, `\`) || strings.Contains(s, "..") || filepath.IsAbs(s) {
		return false
	}
	return true
}
