package tools

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"unicode"
	"unicode/utf8"
)

func makeDiagram(raw json.RawMessage) (any, error) {
	var in struct {
		Title string `json:"title"`
		Nodes []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"nodes"`
		Edges []struct {
			From  string `json:"from"`
			To    string `json:"to"`
			Label string `json:"label"`
		} `json:"edges"`
	}
	decode(raw, &in)
	ids := map[string]int{}
	for i, n := range in.Nodes {
		if strings.TrimSpace(n.ID) == "" {
			return nil, invalid("arguments.nodes", "node ids cannot be blank")
		}
		if _, ok := ids[n.ID]; ok {
			return nil, invalid("arguments.nodes", "node ids must be unique")
		}
		ids[n.ID] = i
	}
	connections := map[[2]string]bool{}
	for _, e := range in.Edges {
		a, ok := ids[e.From]
		b, ok2 := ids[e.To]
		if !ok || !ok2 || a == b {
			return nil, invalid("arguments.edges", "edges must connect two distinct existing node ids")
		}
		pair := [2]string{e.From, e.To}
		if connections[pair] {
			return nil, invalid("arguments.edges", "duplicate directed edges are not supported")
		}
		connections[pair] = true
	}
	for _, label := range append([]string{in.Title}, diagramLabels(in.Nodes)...) {
		if strings.TrimSpace(label) == "" || strings.IndexFunc(label, unicode.IsControl) >= 0 {
			return nil, invalid("arguments", "diagram labels must be nonempty single-line text")
		}
	}
	for _, e := range in.Edges {
		if strings.IndexFunc(e.Label, unicode.IsControl) >= 0 {
			return nil, invalid("arguments.edges", "edge labels must be single-line text")
		}
	}
	height := 100 + len(in.Nodes)*100
	var svg strings.Builder
	fmt.Fprintf(&svg, `<svg xmlns="http://www.w3.org/2000/svg" width="760" height="%d" viewBox="0 0 760 %d" role="img" aria-labelledby="title"><title id="title">%s</title><defs><marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" fill="#475569"/></marker></defs><rect width="760" height="%d" fill="#f8fafc"/>`, height, height, html.EscapeString(in.Title), height)
	titleSize := 22
	if count := utf8.RuneCountInString(in.Title); count > 28 {
		titleSize = 640 / count
	}
	fmt.Fprintf(&svg, `<text x="380" y="42" text-anchor="middle" font-family="sans-serif" font-size="%d" fill="#0f172a">%s</text>`, titleSize, html.EscapeString(in.Title))
	for i, e := range in.Edges {
		a, b := ids[e.From], ids[e.To]
		y1, y2 := 100+a*100+60, 100+b*100
		if b == a+1 {
			fmt.Fprintf(&svg, `<path d="M380 %d L380 %d" fill="none" stroke="#475569" stroke-width="2" marker-end="url(#arrow)"/>`, y1, y2-3)
			if e.Label != "" {
				edgeLabel(&svg, 398, (y1+y2)/2+5, e.Label)
			}
		} else {
			// Alternating side lanes keep branches away from the node interiors.
			x := 60 + (i/2)*8
			anchor := 180
			if i%2 == 1 {
				x = 700 - (i/2)*8
				anchor = 580
			}
			startY, endY := 130+a*100, 130+b*100
			fmt.Fprintf(&svg, `<path d="M%d %d H%d V%d H%d" fill="none" stroke="#475569" stroke-width="2" marker-end="url(#arrow)"/>`, anchor, startY, x, endY, anchor)
			if e.Label != "" {
				edgeLabel(&svg, x+4, (startY+endY)/2, e.Label)
			}
		}
	}
	for i, n := range in.Nodes {
		y := 100 + i*100
		fmt.Fprintf(&svg, `<rect x="180" y="%d" width="400" height="60" rx="12" fill="#ffffff" stroke="#2563eb" stroke-width="2"/>`, y)
		// A conservative font size fits the maximum allowed label without clipping.
		size := 18
		if count := utf8.RuneCountInString(n.Label); count > 20 {
			size = 340 / count
		}
		fmt.Fprintf(&svg, `<text x="380" y="%d" text-anchor="middle" dominant-baseline="middle" font-family="sans-serif" font-size="%d" fill="#0f172a">%s</text>`, y+30, size, html.EscapeString(n.Label))
	}
	svg.WriteString("</svg>")
	return map[string]any{"svg": svg.String(), "media_type": "image/svg+xml", "filename": "diagram.svg"}, nil
}

func diagramLabels(nodes []struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Label
	}
	return out
}
func edgeLabel(svg *strings.Builder, x, y int, label string) {
	fmt.Fprintf(svg, `<text x="%d" y="%d" font-family="sans-serif" font-size="10" fill="#334155">%s</text>`, x, y, html.EscapeString(label))
}
