package tools

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

func makeChecklist(raw json.RawMessage) (any, error) {
	var in struct {
		Title string
		Items []string
	}
	decode(raw, &in)
	if strings.TrimSpace(in.Title) == "" {
		return nil, invalid("arguments.title", "title cannot be blank")
	}
	var out strings.Builder
	fmt.Fprintf(&out, "# %s\n\n", markdownText(in.Title))
	for _, item := range in.Items {
		if strings.TrimSpace(item) == "" {
			return nil, invalid("arguments.items", "checklist items cannot be blank")
		}
		fmt.Fprintf(&out, "- [ ] %s\n", markdownText(item))
	}
	return map[string]any{"filename": "checklist.md", "media_type": "text/markdown; charset=utf-8", "text": out.String(), "count": len(in.Items)}, nil
}

func makeTable(raw json.RawMessage) (any, error) {
	var in struct {
		Headers []string
		Rows    [][]string
	}
	decode(raw, &in)
	for _, header := range in.Headers {
		if strings.TrimSpace(header) == "" {
			return nil, invalid("arguments.headers", "table headers cannot be blank")
		}
	}
	var csvText bytes.Buffer
	writer := csv.NewWriter(&csvText)
	_ = writer.Write(in.Headers)
	var md strings.Builder
	writeRow := func(row []string) {
		md.WriteString("| ")
		for i, value := range row {
			if i > 0 {
				md.WriteString(" | ")
			}
			md.WriteString(markdownText(value))
		}
		md.WriteString(" |\n")
	}
	writeRow(in.Headers)
	separator := make([]string, len(in.Headers))
	for i := range separator {
		separator[i] = "---"
	}
	writeRow(separator)
	for _, row := range in.Rows {
		if len(row) != len(in.Headers) {
			return nil, invalid("arguments.rows", "every row must match the header count")
		}
		_ = writer.Write(row)
		writeRow(row)
	}
	writer.Flush()
	if writer.Error() != nil {
		return nil, invalid("arguments.rows", "could not create CSV")
	}
	return map[string]any{"rows": len(in.Rows), "columns": len(in.Headers), "files": []map[string]string{{"filename": "table.csv", "media_type": "text/csv; charset=utf-8", "text": csvText.String()}, {"filename": "table.md", "media_type": "text/markdown; charset=utf-8", "text": md.String()}}}, nil
}

func makeIdenticon(raw json.RawMessage) (any, error) {
	var in struct{ Seed string }
	decode(raw, &in)
	sum := sha256.Sum256([]byte(in.Seed))
	color := fmt.Sprintf("#%02x%02x%02x", 48+sum[0]%144, 48+sum[1]%144, 48+sum[2]%144)
	var svg strings.Builder
	svg.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="280" height="280" viewBox="0 0 280 280" role="img"><title>Generated identicon</title><rect width="280" height="280" rx="24" fill="#f1f5f9"/>`)
	for row := 0; row < 5; row++ {
		for col := 0; col < 3; col++ {
			if sum[3+row*3+col]&1 == 0 {
				continue
			}
			fmt.Fprintf(&svg, `<rect x="%d" y="%d" width="40" height="40" rx="5" fill="%s"/>`, 30+col*44, 30+row*44, color)
			if col < 2 {
				fmt.Fprintf(&svg, `<rect x="%d" y="%d" width="40" height="40" rx="5" fill="%s"/>`, 30+(4-col)*44, 30+row*44, color)
			}
		}
	}
	svg.WriteString("</svg>")
	return map[string]any{"filename": "identicon.svg", "media_type": "image/svg+xml", "svg": svg.String(), "color": color}, nil
}

func colorContrast(raw json.RawMessage) (any, error) {
	var in struct{ Foreground, Background string }
	decode(raw, &in)
	luminance := func(s string) (float64, error) {
		if len(s) != 7 || s[0] != '#' {
			return 0, invalid("arguments", "colors must use #RRGGBB format")
		}
		value, err := hex.DecodeString(s[1:])
		if err != nil {
			return 0, invalid("arguments", "colors must contain hexadecimal digits")
		}
		linear := func(b byte) float64 {
			c := float64(b) / 255
			if c <= .04045 {
				return c / 12.92
			}
			return math.Pow((c+.055)/1.055, 2.4)
		}
		return .2126*linear(value[0]) + .7152*linear(value[1]) + .0722*linear(value[2]), nil
	}
	a, err := luminance(in.Foreground)
	if err != nil {
		return nil, err
	}
	b, err := luminance(in.Background)
	if err != nil {
		return nil, err
	}
	light, dark := math.Max(a, b), math.Min(a, b)
	ratio := (light + .05) / (dark + .05)
	return map[string]any{"ratio": ratio, "aa_normal_text": ratio >= 4.5, "aa_large_text": ratio >= 3, "aaa_normal_text": ratio >= 7, "aaa_large_text": ratio >= 4.5}, nil
}

var zoneName = regexp.MustCompile(`^[A-Za-z0-9_+-]+(/[A-Za-z0-9_+-]+)*$`)
var timestampFormat = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|[+-](0\d|1\d|2[0-3]):[0-5]\d)$`)

func parseTimestamp(value string) (time.Time, error) {
	if !timestampFormat.MatchString(value) {
		return time.Time{}, invalid("arguments", "use a whole-second RFC3339 timestamp with an explicit valid UTC offset")
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil || t.Year() < 1 {
		return time.Time{}, invalid("arguments", "invalid RFC3339 timestamp")
	}
	return t, nil
}

func timeConvert(raw json.RawMessage) (any, error) {
	var in struct{ Timestamp, Timezone string }
	decode(raw, &in)
	t, err := parseTimestamp(in.Timestamp)
	if err != nil {
		return nil, invalid("arguments.timestamp", err.Error())
	}
	if in.Timezone == "Local" || !zoneName.MatchString(in.Timezone) {
		return nil, invalid("arguments.timezone", "use UTC or an IANA time-zone identifier")
	}
	location, err := time.LoadLocation(in.Timezone)
	if err != nil {
		return nil, invalid("arguments.timezone", "unknown IANA time-zone identifier")
	}
	local, utc := t.In(location), t.UTC()
	if local.Year() < 1 || local.Year() > 9999 || utc.Year() < 1 || utc.Year() > 9999 {
		return nil, invalid("arguments.timestamp", "converted date is outside years 0001 to 9999")
	}
	abbreviation, offset := local.Zone()
	return map[string]any{"local": local.Format(time.RFC3339), "utc": utc.Format(time.RFC3339), "timezone": in.Timezone, "abbreviation": abbreviation, "offset_seconds": offset, "weekday": local.Weekday().String(), "unix_seconds": t.Unix()}, nil
}

func inspectURL(raw json.RawMessage) (any, error) {
	var in struct {
		URL string `json:"url"`
	}
	decode(raw, &in)
	u, err := url.Parse(in.URL)
	if u != nil {
		u.Scheme = strings.ToLower(u.Scheme)
	}
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Opaque != "" {
		return nil, invalid("arguments.url", "use an absolute HTTP or HTTPS URL")
	}
	if u.User != nil {
		return nil, invalid("arguments.url", "URLs with embedded credentials are not accepted")
	}
	port := u.Port()
	if port != "" {
		p, err := strconv.Atoi(port)
		if err != nil || p < 1 || p > 65535 {
			return nil, invalid("arguments.url", "port must be from 1 to 65535")
		}
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, invalid("arguments.url", "query string has invalid escaping or separators")
	}
	return map[string]any{"scheme": u.Scheme, "hostname": u.Hostname(), "port": port, "path": u.Path, "query": query, "fragment": u.Fragment, "fetched": false}, nil
}

func inspectImage(raw json.RawMessage) (any, error) {
	var in struct {
		Base64 string `json:"base64"`
	}
	decode(raw, &in)
	data, err := base64.StdEncoding.Strict().DecodeString(in.Base64)
	if err != nil || len(data) > 128*1024 {
		return nil, invalid("arguments.base64", "send standard base64 image data decoding to at most 128 KiB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 {
		return nil, invalid("arguments.base64", "image header is not a recognized PNG, JPEG, or GIF")
	}
	return map[string]any{"width": config.Width, "height": config.Height, "format": format, "input_bytes": len(data), "aspect_ratio": float64(config.Width) / float64(config.Height), "complete_image_verified": false}, nil
}

func makeTone(raw json.RawMessage) (any, error) {
	var in struct {
		Frequency float64 `json:"frequency_hz"`
		Duration  float64 `json:"duration_seconds"`
	}
	decode(raw, &in)
	const rate = 16000
	samples := int(math.Round(in.Duration * rate))
	data := make([]byte, 44+samples*2)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
	copy(data[8:12], "WAVE")
	copy(data[12:16], "fmt ")
	binary.LittleEndian.PutUint32(data[16:20], 16)
	binary.LittleEndian.PutUint16(data[20:22], 1)
	binary.LittleEndian.PutUint16(data[22:24], 1)
	binary.LittleEndian.PutUint32(data[24:28], rate)
	binary.LittleEndian.PutUint32(data[28:32], rate*2)
	binary.LittleEndian.PutUint16(data[32:34], 2)
	binary.LittleEndian.PutUint16(data[34:36], 16)
	copy(data[36:40], "data")
	binary.LittleEndian.PutUint32(data[40:44], uint32(samples*2))
	const fade = 160
	for i := 0; i < samples; i++ {
		envelope := math.Min(1, math.Min(float64(i)/fade, float64(samples-1-i)/fade))
		sample := int16(math.Round(.2 * 32767 * envelope * math.Sin(2*math.Pi*in.Frequency*float64(i)/rate)))
		binary.LittleEndian.PutUint16(data[44+i*2:], uint16(sample))
	}
	return map[string]any{"filename": "tone.wav", "media_type": "audio/wav", "base64": base64.StdEncoding.EncodeToString(data), "sample_rate": rate, "channels": 1, "duration_seconds": float64(samples) / rate, "frequency_hz": in.Frequency}, nil
}

func normalizeCSV(raw json.RawMessage) (any, error) {
	var in struct{ Text, From, To string }
	decode(raw, &in)
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(in.Text, "\ufeff")))
	if in.From == "tsv" {
		reader.Comma = '\t'
	}
	var out bytes.Buffer
	writer := csv.NewWriter(&out)
	if in.To == "tsv" {
		writer.Comma = '\t'
	}
	rows, columns := 0, 0
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(record) == 0 || len(record) > 32 {
			return nil, invalid("arguments.text", "invalid quoting or inconsistent/oversized rows")
		}
		rows++
		if rows > 1001 {
			return nil, invalid("arguments.text", "input exceeds 1000 data rows")
		}
		columns = len(record)
		for _, cell := range record {
			if len(cell) > 4096 {
				return nil, invalid("arguments.text", "cell exceeds 4096 bytes")
			}
		}
		_ = writer.Write(record)
	}
	if rows == 0 {
		return nil, invalid("arguments.text", "input must contain a header row")
	}
	writer.Flush()
	if writer.Error() != nil {
		return nil, invalid("arguments.text", "could not write delimited text")
	}
	mediaType := "text/csv; charset=utf-8"
	if in.To == "tsv" {
		mediaType = "text/tab-separated-values; charset=utf-8"
	}
	return map[string]any{"filename": "table." + in.To, "media_type": mediaType, "text": out.String(), "data_rows": rows - 1, "columns": columns}, nil
}

func generateUUID(raw json.RawMessage) (any, error) {
	var in struct{ Count float64 }
	decode(raw, &in)
	ids := make([]string, int(in.Count))
	for i := range ids {
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, &Error{Code: "tool_failed", Message: "could not generate random UUID"}
		}
		b[6] = (b[6] & 0x0f) | 0x40
		b[8] = (b[8] & 0x3f) | 0x80
		ids[i] = fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
	}
	return map[string]any{"uuids": ids, "version": 4}, nil
}
