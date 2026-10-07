package tools

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/png"
	"math"
	"strconv"
	"strings"
)

func nonblank(s, field string) error {
	if strings.TrimSpace(s) == "" {
		return invalid(field, "text must not be blank")
	}
	return nil
}

func makePixelArt(raw json.RawMessage) (any, error) {
	var in struct {
		Palette []string
		Pixels  [][]float64
		Scale   float64
	}
	decode(raw, &in)
	colors := make([]color.NRGBA, len(in.Palette))
	for i, s := range in.Palette {
		if (len(s) != 7 && len(s) != 9) || s[0] != '#' {
			return nil, invalid(fmt.Sprintf("arguments.palette[%d]", i), "use #RRGGBB or #RRGGBBAA")
		}
		b, err := hex.DecodeString(s[1:])
		if err != nil {
			return nil, invalid("arguments.palette", "invalid hexadecimal color")
		}
		colors[i] = color.NRGBA{R: b[0], G: b[1], B: b[2], A: 255}
		if len(b) == 4 {
			colors[i].A = b[3]
		}
	}
	w, h, scale := len(in.Pixels[0]), len(in.Pixels), int(in.Scale)
	img := image.NewNRGBA(image.Rect(0, 0, w*scale, h*scale))
	var svg strings.Builder
	fmt.Fprintf(&svg, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" shape-rendering="crispEdges">`, w*scale, h*scale, w, h)
	for y, row := range in.Pixels {
		if len(row) != w {
			return nil, invalid("arguments.pixels", "every row must have the same width")
		}
		for x, index := range row {
			if int(index) >= len(colors) {
				return nil, invalid(fmt.Sprintf("arguments.pixels[%d][%d]", y, x), "palette index is outside the supplied palette")
			}
			c := colors[int(index)]
			if c.A > 0 {
				fmt.Fprintf(&svg, `<rect x="%d" y="%d" width="1" height="1" fill="#%02x%02x%02x" fill-opacity="%.8f"/>`, x, y, c.R, c.G, c.B, float64(c.A)/255)
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.SetNRGBA(x*scale+dx, y*scale+dy, c)
				}
			}
		}
	}
	svg.WriteString("</svg>")
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, &Error{Code: "tool_failed", Message: "could not encode PNG"}
	}
	return map[string]any{"width": w * scale, "height": h * scale, "grid_width": w, "grid_height": h, "png": map[string]any{"filename": "sprite.png", "media_type": "image/png", "base64": base64.StdEncoding.EncodeToString(buf.Bytes())}, "svg": map[string]any{"filename": "sprite.svg", "media_type": "image/svg+xml", "text": svg.String()}}, nil
}

func wavPCM(samples []int16) []byte {
	data := make([]byte, 44+len(samples)*2)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:20], 16)
	binary.LittleEndian.PutUint16(data[20:22], 1)
	binary.LittleEndian.PutUint16(data[22:24], 1)
	binary.LittleEndian.PutUint32(data[24:28], 16000)
	binary.LittleEndian.PutUint32(data[28:32], 32000)
	binary.LittleEndian.PutUint16(data[32:34], 2)
	binary.LittleEndian.PutUint16(data[34:36], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:44], uint32(len(samples)*2))
	for i, s := range samples {
		binary.LittleEndian.PutUint16(data[44+i*2:], uint16(s))
	}
	return data
}

func makeMusicSequence(raw json.RawMessage) (any, error) {
	var in struct {
		BPM   float64
		Notes []struct {
			Pitch string
			Beats float64
		}
	}
	decode(raw, &in)
	samples := make([]int16, 0)
	events := make([]map[string]any, 0, len(in.Notes))
	elapsed := 0.0
	for i, n := range in.Notes {
		frequency := 0.0
		if n.Pitch != "rest" {
			midi, err := strconv.Atoi(n.Pitch)
			if err != nil || midi < 48 || midi > 96 || strconv.Itoa(midi) != n.Pitch {
				return nil, invalid(fmt.Sprintf("arguments.notes[%d].pitch", i), "use a MIDI number string from 48 to 96, or rest")
			}
			frequency = 440 * math.Pow(2, float64(midi-69)/12)
		}
		start := len(samples)
		elapsed += n.Beats * 60 / in.BPM
		if elapsed > 16+1e-12 {
			return nil, invalid("arguments.notes", "total duration must be at most 16 seconds")
		}
		end := int(math.Round(elapsed * 16000))
		count := end - start
		events = append(events, map[string]any{"pitch": n.Pitch, "frequency_hz": frequency, "start_sample": start, "samples": count, "start_seconds": float64(start) / 16000, "duration_seconds": float64(count) / 16000})
		for j := 0; j < count; j++ {
			envelope := math.Min(1, math.Min(float64(j)/160, float64(count-1-j)/160))
			samples = append(samples, int16(math.Round(.2*32767*envelope*math.Sin(2*math.Pi*frequency*float64(j)/16000))))
		}
	}
	return map[string]any{"filename": "melody.wav", "media_type": "audio/wav", "base64": base64.StdEncoding.EncodeToString(wavPCM(samples)), "sample_rate": 16000, "channels": 1, "duration_seconds": float64(len(samples)) / 16000, "notes": events}, nil
}

type projectTask struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Duration float64  `json:"duration_minutes"`
	Depends  []string `json:"depends_on"`
}

// neutralizeCSV keeps exported user text from becoming spreadsheet formulas.
func neutralizeCSV(s string) string {
	t := strings.TrimLeft(s, " \t\r\n")
	if t != "" && strings.ContainsRune("=+-@", rune(t[0])) {
		return "'" + s
	}
	return s
}

func planProject(raw json.RawMessage) (any, error) {
	var in struct{ Tasks []projectTask }
	decode(raw, &in)
	n := len(in.Tasks)
	index := map[string]int{}
	children := make([][]int, n)
	indegree := make([]int, n)
	total := 0
	for i, t := range in.Tasks {
		if err := nonblank(t.ID, "arguments.tasks.id"); err != nil {
			return nil, err
		}
		if err := nonblank(t.Title, "arguments.tasks.title"); err != nil {
			return nil, err
		}
		if _, exists := index[t.ID]; exists {
			return nil, invalid("arguments.tasks.id", "task IDs must be unique")
		}
		index[t.ID] = i
		total += int(t.Duration)
	}
	for i, t := range in.Tasks {
		seen := map[string]bool{}
		for _, id := range t.Depends {
			j, ok := index[id]
			if !ok || j == i || seen[id] {
				return nil, invalid("arguments.tasks.depends_on", "dependencies must be unique existing IDs other than the task itself")
			}
			seen[id] = true
			children[j] = append(children[j], i)
			indegree[i]++
		}
	}
	order := make([]int, 0, n)
	for i, d := range indegree {
		if d == 0 {
			order = append(order, i)
		}
	}
	start, finish := make([]int, n), make([]int, n)
	duration := 0
	for k := 0; k < len(order); k++ {
		i := order[k]
		finish[i] = start[i] + int(in.Tasks[i].Duration)
		duration = max(duration, finish[i])
		for _, j := range children[i] {
			start[j] = max(start[j], finish[i])
			indegree[j]--
			if indegree[j] == 0 {
				order = append(order, j)
			}
		}
	}
	if len(order) != n {
		return nil, invalid("arguments.tasks.depends_on", "dependency cycle detected; remove a circular dependency")
	}
	latest := make([]int, n)
	for i := range latest {
		latest[i] = duration
	}
	for k := len(order) - 1; k >= 0; k-- {
		i := order[k]
		for _, j := range children[i] {
			latest[i] = min(latest[i], latest[j]-int(in.Tasks[j].Duration))
		}
	}
	rows := make([]map[string]any, 0, n)
	critical := make([]string, 0)
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	_ = writer.Write([]string{"id", "title", "earliest_start_minutes", "earliest_finish_minutes", "latest_start_minutes", "latest_finish_minutes", "slack_minutes", "critical"})
	for _, i := range order {
		t := in.Tasks[i]
		ls := latest[i] - int(t.Duration)
		slack := ls - start[i]
		if slack == 0 {
			critical = append(critical, t.ID)
		}
		rows = append(rows, map[string]any{"id": t.ID, "title": t.Title, "earliest_start_minutes": start[i], "earliest_finish_minutes": finish[i], "latest_start_minutes": ls, "latest_finish_minutes": latest[i], "slack_minutes": slack, "critical": slack == 0})
		_ = writer.Write([]string{neutralizeCSV(t.ID), neutralizeCSV(t.Title), strconv.Itoa(start[i]), strconv.Itoa(finish[i]), strconv.Itoa(ls), strconv.Itoa(latest[i]), strconv.Itoa(slack), strconv.FormatBool(slack == 0)})
	}
	writer.Flush()
	return map[string]any{"duration_minutes": duration, "total_work_minutes": total, "critical_task_ids": critical, "schedule": rows, "assumptions": "unlimited parallel workers; elapsed minutes; no resource, calendar, or holiday constraints", "csv": buf.String(), "filename": "project.csv", "media_type": "text/csv; charset=utf-8", "csv_formula_text_escaped": true}, nil
}

func appResult(title, kind string, data any, body, script string) (map[string]any, error) {
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(encoded)
	key := "edsger-" + kind + "-" + hex.EncodeToString(sum[:16])
	code := "'use strict';\nconst DATA=" + string(encoded) + ";\nconst STORAGE_KEY=" + strconv.Quote(key) + ";\n" + appCommonJS + script
	hash := sha256.Sum256([]byte(code))
	csp := "default-src 'none'; script-src 'sha256-" + base64.StdEncoding.EncodeToString(hash[:]) + "'; style-src 'unsafe-inline'; img-src data: blob:; connect-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'"
	text := `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta http-equiv="Content-Security-Policy" content="` + html.EscapeString(csp) + `"><title>` + html.EscapeString(title) + `</title><style>` + appCSS + `</style></head><body><main><header><p class="brand">MADE WITH EDSGER · WORKS OFFLINE</p><h1>` + html.EscapeString(title) + `</h1></header>` + body + `<noscript>This file needs JavaScript enabled to use its interactive controls.</noscript><footer>Self-contained export. No network requests. Your data stays in this file and browser.</footer></main><script>` + code + `</script></body></html>`
	return map[string]any{"filename": kind + ".html", "media_type": "text/html; charset=utf-8", "html": text, "offline": true, "network_requests": false, "preview_requires_scripts": true, "content_verified": false}, nil
}
