package tools

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"regexp"
	"strings"
	"testing"
)

func TestCreativeTools(t *testing.T) {
	s := callOK(t, "color_contrast", `{"foreground":"#000000","background":"#FFFFFF"}`)
	closeTo(t, s["ratio"], 21)
	if s["aaa_normal_text"] != true {
		t.Fatal(s)
	}
	s = callOK(t, "color_contrast", `{"foreground":"#123456","background":"#123456"}`)
	closeTo(t, s["ratio"], 1)
	if s["aa_large_text"] != false {
		t.Fatal(s)
	}
	s = callOK(t, "time_convert", `{"timestamp":"2026-01-07T20:00:00Z","timezone":"America/Chicago"}`)
	if s["local"] != "2026-01-07T14:00:00-06:00" {
		t.Fatal(s)
	}
	s = callOK(t, "time_convert", `{"timestamp":"2026-07-07T20:00:00Z","timezone":"America/Chicago"}`)
	if s["local"] != "2026-07-07T15:00:00-05:00" {
		t.Fatal(s)
	}
	s = callOK(t, "inspect_url", `{"url":"https://example.com:8443/a%20b?q=one+two&q=three#here"}`)
	if s["path"] != "/a b" || s["port"] != "8443" || s["fetched"] != false {
		t.Fatal(s)
	}
	values := s["query"].(map[string]any)["q"].([]any)
	if len(values) != 2 || values[0] != "one two" {
		t.Fatal(s)
	}
	s = callOK(t, "make_checklist", `{"title":"Prep","items":["Read notes","<script>x</script>"]}`)
	if !strings.Contains(s["text"].(string), "- [ ] Read notes") || strings.Contains(s["text"].(string), "<script>") {
		t.Fatal(s)
	}
	a := callOK(t, "make_identicon", `{"seed":"Edsger"}`)
	b := callOK(t, "make_identicon", `{"seed":"Edsger"}`)
	if a["svg"] != b["svg"] {
		t.Fatal("identicon is not deterministic")
	}
	s = callOK(t, "make_table", `{"headers":["A","B"],"rows":[["one|two","three, four"],["five\nsix","seven"]]}`)
	files := s["files"].([]any)
	reader := csv.NewReader(strings.NewReader(files[0].(map[string]any)["text"].(string)))
	records, err := reader.ReadAll()
	if err != nil || records[2][0] != "five\nsix" {
		t.Fatal(records, err)
	}
	if !strings.Contains(files[1].(map[string]any)["text"].(string), `one\|two`) {
		t.Fatal("Markdown pipe not escaped")
	}
	s = callOK(t, "normalize_csv", `{"text":"name,note\nC,\"a\tb\"\nPython,\"one\ntwo\"","from":"csv","to":"tsv"}`)
	raw, _ := json.Marshal(map[string]string{"text": s["text"].(string), "from": "tsv", "to": "csv"})
	s = callOK(t, "normalize_csv", string(raw))
	reader = csv.NewReader(strings.NewReader(s["text"].(string)))
	records, err = reader.ReadAll()
	if err != nil || records[1][1] != "a\tb" || records[2][1] != "one\ntwo" {
		t.Fatal(records, err)
	}
}

func TestImageHeaderAndTone(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"base64": base64.StdEncoding.EncodeToString(out.Bytes())})
	s := callOK(t, "inspect_image", string(raw))
	closeTo(t, s["width"], 3)
	closeTo(t, s["height"], 2)
	if s["format"] != "png" || s["complete_image_verified"] != false {
		t.Fatal(s)
	}
	s = callOK(t, "make_tone", `{"frequency_hz":440,"duration_seconds":0.5}`)
	wav, err := base64.StdEncoding.DecodeString(s["base64"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if len(wav) != 16044 || string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" || binary.LittleEndian.Uint32(wav[24:28]) != 16000 || int(binary.LittleEndian.Uint32(wav[40:44])) != len(wav)-44 {
		t.Fatal("invalid WAV header")
	}
	if binary.LittleEndian.Uint16(wav[44:46]) != 0 || binary.LittleEndian.Uint16(wav[len(wav)-2:]) != 0 {
		t.Fatal("tone did not fade to zero")
	}
	for i := 44; i < len(wav); i += 2 {
		if math.Abs(float64(int16(binary.LittleEndian.Uint16(wav[i:])))) > 6554 {
			t.Fatal("unexpected amplitude")
		}
	}
	// Count positive-going zero crossings away from the faded endpoints.
	crossings := 0
	for i := 46; i < len(wav); i += 2 {
		prev := int16(binary.LittleEndian.Uint16(wav[i-2:]))
		now := int16(binary.LittleEndian.Uint16(wav[i:]))
		if prev <= 0 && now > 0 {
			crossings++
		}
	}
	if crossings < 219 || crossings > 221 {
		t.Fatalf("frequency is wrong: %d crossings", crossings)
	}
}

func TestUUIDBits(t *testing.T) {
	s := callOK(t, "generate_uuid", `{"count":100}`)
	ids := s["uuids"].([]any)
	seen := map[string]bool{}
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	for _, value := range ids {
		id := value.(string)
		if seen[id] || !pattern.MatchString(id) {
			t.Fatal("invalid or duplicate UUID", id)
		}
		seen[id] = true
	}
}

func TestCreativeToolRejections(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"color_contrast", `{"foreground":"#fff","background":"#ffffff"}`}, {"color_contrast", `{"foreground":"#zzzzzz","background":"#ffffff"}`},
		{"time_convert", `{"timestamp":"2026-01-01T00:00:00Z","timezone":"../etc/passwd"}`}, {"time_convert", `{"timestamp":"2026-01-01T00:00:00Z","timezone":"Local"}`}, {"time_convert", `{"timestamp":"2026-01-01T00:00:00Z","timezone":"No/Such_Zone"}`}, {"time_convert", `{"timestamp":"2026-01-01T00:00:00+24:00","timezone":"UTC"}`},
		{"inspect_url", `{"url":"file:///etc/passwd"}`}, {"inspect_url", `{"url":"https://user:secret@example.com"}`}, {"inspect_url", `{"url":"https://example.com:99999"}`}, {"inspect_url", `{"url":"https://example.com?q=%xx"}`},
		{"inspect_image", `{"base64":"bm90IGFuIGltYWdl"}`}, {"inspect_image", `{"base64":"!"}`},
		{"make_tone", `{"frequency_hz":0,"duration_seconds":1}`}, {"make_tone", `{"frequency_hz":440,"duration_seconds":3}`},
		{"make_checklist", `{"title":"x","items":[""]}`}, {"make_table", `{"headers":["a","b"],"rows":[["one"]]}`}, {"normalize_csv", `{"text":"a,b\n1","from":"csv","to":"tsv"}`}, {"generate_uuid", `{"count":101}`},
	} {
		if _, err := Call(tc.name, []byte(tc.raw)); err == nil {
			t.Fatalf("accepted invalid %s: %s", tc.name, tc.raw)
		}
	}
}
