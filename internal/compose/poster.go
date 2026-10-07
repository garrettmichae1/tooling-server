// Package compose turns a short poster brief into a designed sequence of drawing calls.
package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// ErrBrief means the poster arguments are not usable.
var ErrBrief = errors.New("invalid brief")

// Caller is the headless renderer.
type Caller interface {
	Call(ctx context.Context, name string, arguments json.RawMessage) (json.RawMessage, error)
}

type palette struct {
	Sky, Halo, Moon, Glow, Ridge, Building, Window, Title, Sub string
}

var palettes = map[string]palette{
	"night": {Sky: "#070b16", Halo: "#243044", Moon: "#f6e7c1", Glow: "#f4e4b2", Ridge: "#10182c", Building: "#0c1222", Window: "#f0d78c", Title: "#f7f4ee", Sub: "#8eb4ff"},
	"dusk":  {Sky: "#1a1030", Halo: "#5a2a48", Moon: "#ffb085", Glow: "#ff8a5b", Ridge: "#2a1844", Building: "#1a1030", Window: "#ffd2a8", Title: "#fff6ef", Sub: "#ffb085"},
	"paper": {Sky: "#f4f0e6", Halo: "#e7e0d2", Moon: "#f7f4ee", Glow: "#d9d0c1", Ridge: "#2c2824", Building: "#3a342c", Window: "#f4f0e6", Title: "#1c1916", Sub: "#6b5344"},
}

// Poster draws a composed poster and returns a short summary. It does not forward the brief to VectorCraft.
func Poster(ctx context.Context, c Caller, raw json.RawMessage) (json.RawMessage, error) {
	brief, pal, err := parse(raw)
	if err != nil {
		return nil, ErrBrief
	}
	shape := func(args map[string]any) (json.RawMessage, error) {
		args["stroke"] = "none"
		return call(ctx, c, "draw_shape", args)
	}
	if _, err := shape(rect(0, 0, 612, 792, pal.Sky)); err != nil {
		return nil, err
	}
	if _, err := shape(ellipse(80, 40, 460, 460, pal.Halo)); err != nil {
		return nil, err
	}
	for _, star := range stars {
		if _, err := shape(ellipse(star[0], star[1], star[2], star[2], pal.Title)); err != nil {
			return nil, err
		}
	}
	moon, err := shape(ellipse(196, 118, 220, 220, pal.Moon))
	if err != nil {
		return nil, err
	}
	if id := objectID(moon); id != nil {
		glow, _ := json.Marshal(map[string]any{
			"effect": "stylize.outerGlow",
			"params": map[string]any{"color": pal.Glow, "opacity": 70, "blur": 18},
			"ids":    []json.RawMessage{id},
		})
		_, _ = c.Call(ctx, "apply_effect", glow)
	}
	if _, err := call(ctx, c, "draw_path", map[string]any{
		"points": [][]float64{{0, 500}, {90, 430}, {170, 490}, {260, 390}, {360, 480}, {450, 360}, {540, 470}, {612, 420}, {612, 792}, {0, 792}},
		"closed": true, "fill": pal.Ridge, "stroke": "none",
	}); err != nil {
		return nil, err
	}
	for _, b := range buildings {
		if _, err := shape(rect(b[0], b[1], b[2], b[3], pal.Building)); err != nil {
			return nil, err
		}
		windows(shape, b, pal.Window)
	}
	if _, err := call(ctx, c, "add_text", map[string]any{"text": brief.Title, "x": 48, "y": 64, "size": 46, "color": pal.Title}); err != nil {
		return nil, err
	}
	if brief.Subtitle != "" {
		if _, err := call(ctx, c, "add_text", map[string]any{"text": brief.Subtitle, "x": 50, "y": 124, "size": 16, "color": pal.Sub}); err != nil {
			return nil, err
		}
	}
	return json.Marshal(map[string]any{"ok": true, "mood": brief.Mood, "title": brief.Title})
}

type brief struct {
	Title, Subtitle, Mood string
}

func parse(raw json.RawMessage) (brief, palette, error) {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var b brief
	if err := dec.Decode(&b); err != nil {
		return brief{}, palette{}, ErrBrief
	}
	b.Title = strings.TrimSpace(b.Title)
	b.Subtitle = strings.TrimSpace(b.Subtitle)
	b.Mood = strings.TrimSpace(b.Mood)
	if b.Title == "" {
		return brief{}, palette{}, ErrBrief
	}
	if b.Mood == "" {
		b.Mood = "night"
	}
	pal, ok := palettes[b.Mood]
	if !ok {
		return brief{}, palette{}, ErrBrief
	}
	return b, pal, nil
}

func call(ctx context.Context, c Caller, name string, args map[string]any) (json.RawMessage, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	return c.Call(ctx, name, raw)
}

func objectID(result json.RawMessage) json.RawMessage {
	var env struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(result, &env) != nil {
		return nil
	}
	for _, part := range env.Content {
		var body struct {
			ID json.RawMessage `json:"id"`
		}
		if json.Unmarshal([]byte(part.Text), &body) != nil || len(body.ID) == 0 || body.ID[0] == '"' {
			continue
		}
		return body.ID
	}
	return nil
}

func rect(x, y, w, h float64, fill string) map[string]any {
	return map[string]any{"shape": "rectangle", "x": x, "y": y, "width": w, "height": h, "fill": fill}
}

func ellipse(x, y, w, h float64, fill string) map[string]any {
	return map[string]any{"shape": "ellipse", "x": x, "y": y, "width": w, "height": h, "fill": fill}
}

func windows(shape func(map[string]any) (json.RawMessage, error), b [4]float64, fill string) {
	x, y, w, h := b[0], b[1], b[2], b[3]
	n := 0
	for row := y + 18; row < y+h-28 && n < 8; row += 26 {
		for col := x + 14; col < x+w-18 && n < 8; col += 18 {
			if int(row+col)%44 < 16 {
				continue
			}
			_, _ = shape(rect(col, row, 8, 12, fill))
			n++
		}
	}
}

var stars = [][3]float64{
	{40, 150, 2}, {88, 210, 3}, {130, 96, 2}, {470, 80, 3}, {520, 160, 2},
	{560, 240, 2}, {300, 70, 2}, {360, 190, 3}, {70, 300, 2}, {540, 320, 2},
}

var buildings = [][4]float64{
	{32, 548, 72, 180},
	{118, 500, 64, 228},
	{196, 528, 96, 200},
	{308, 468, 58, 260},
	{380, 536, 88, 192},
	{484, 492, 78, 236},
}
