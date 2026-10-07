package tools

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"image/color"
	"image/png"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestPocketValidation(t *testing.T) {
	cases := []struct{ name, raw string }{
		{"make_study_app", `{"title":"x","questions":[{"prompt":"Q","choices":["a","b"],"correct_index":2,"explanation":""}]}`},
		{"make_study_app", `{"title":"x","questions":[{"prompt":"Q","choices":[" A ","a"],"correct_index":0,"explanation":""}]}`},
		{"make_study_app", `{"title":" ","questions":[{"prompt":"Q","choices":["a","b"],"correct_index":0,"explanation":""}]}`},
		{"make_data_dashboard", `{"title":"x","labels":["A","A"],"series":[{"name":"S","values":[1,2]}]}`},
		{"make_data_dashboard", `{"title":"x","labels":["A"],"series":[{"name":"S","values":[1,2]}]}`},
		{"make_data_dashboard", `{"title":"x","labels":["A"],"series":[{"name":"S","values":[1]},{"name":"S","values":[2]}]}`},
		{"make_pixel_art", `{"palette":["#123456"],"pixels":[[0,0],[0]],"scale":1}`},
		{"make_pixel_art", `{"palette":["#123456"],"pixels":[[1]],"scale":1}`},
		{"make_pixel_art", `{"palette":["#GGGGGG"],"pixels":[[0]],"scale":1}`},
		{"make_music_sequence", `{"bpm":30,"notes":[{"pitch":"69","beats":4},{"pitch":"60","beats":4},{"pitch":"rest","beats":1}]}`},
		{"make_music_sequence", `{"bpm":120,"notes":[{"pitch":"C4","beats":1}]}`},
		{"make_music_sequence", `{"bpm":120,"notes":[{"pitch":"+69","beats":1}]}`},
		{"make_music_sequence", `{"bpm":120,"notes":[{"pitch":"97","beats":1}]}`},
		{"plan_project", `{"tasks":[{"id":"a","title":"A","duration_minutes":1,"depends_on":["b"]}]}`},
		{"plan_project", `{"tasks":[{"id":"a","title":"A","duration_minutes":1,"depends_on":["b"]},{"id":"b","title":"B","duration_minutes":1,"depends_on":["a"]}]}`},
		{"plan_project", `{"tasks":[{"id":"a","title":"A","duration_minutes":1,"depends_on":[]},{"id":"a","title":"B","duration_minutes":1,"depends_on":[]}]}`},
		{"plan_project", `{"tasks":[{"id":"a","title":"A","duration_minutes":1,"depends_on":["a"]}]}`},
	}
	for _, tc := range cases {
		if _, err := Call(tc.name, []byte(tc.raw)); err == nil {
			t.Errorf("accepted %s: %s", tc.name, tc.raw)
		}
	}
}

func TestPixelArtPNGAndSVG(t *testing.T) {
	s := callOK(t, "make_pixel_art", `{"palette":["#00000000","#FF000080","#00FF00"],"pixels":[[0,1],[2,0]],"scale":3}`)
	b, _ := base64.StdEncoding.DecodeString(s["png"].(map[string]any)["base64"].(string))
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 6 || img.Bounds().Dy() != 6 {
		t.Fatal(img.Bounds())
	}
	for y := 0; y < 6; y++ {
		for x := 0; x < 6; x++ {
			want := color.NRGBA{}
			if y < 3 && x >= 3 {
				want = color.NRGBA{255, 0, 0, 128}
			}
			if y >= 3 && x < 3 {
				want = color.NRGBA{0, 255, 0, 255}
			}
			got := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if got != want {
				t.Fatalf("pixel %d,%d: %+v != %+v", x, y, got, want)
			}
		}
	}
	d := xml.NewDecoder(strings.NewReader(s["svg"].(map[string]any)["text"].(string)))
	rects := 0
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if e, ok := token.(xml.StartElement); ok && e.Name.Local == "rect" {
			rects++
		}
	}
	if rects != 2 {
		t.Fatal(rects)
	}
}

func TestMusicPitchRestsFadesAndDuration(t *testing.T) {
	s := callOK(t, "make_music_sequence", `{"bpm":120,"notes":[{"pitch":"69","beats":1},{"pitch":"rest","beats":1},{"pitch":"81","beats":1}]}`)
	b, _ := base64.StdEncoding.DecodeString(s["base64"].(string))
	if len(b) != 48044 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" || binary.LittleEndian.Uint32(b[40:44]) != 48000 {
		t.Fatal("bad WAV")
	}
	closeTo(t, s["duration_seconds"], 1.5)
	events := s["notes"].([]any)
	closeTo(t, events[0].(map[string]any)["frequency_hz"], 440)
	closeTo(t, events[2].(map[string]any)["frequency_hz"], 880)
	for i := 0; i < 24000; i++ {
		v := int16(binary.LittleEndian.Uint16(b[44+i*2:]))
		if math.Abs(float64(v)) > 6554 {
			t.Fatal("amplitude")
		}
		if i >= 8000 && i < 16000 && v != 0 {
			t.Fatal("non-silent rest")
		}
		if (i%8000 == 0 || i%8000 == 7999) && v != 0 {
			t.Fatal("fade endpoint")
		}
	}
	cycles := 0
	for i := 16161; i < 23839; i++ {
		a := int16(binary.LittleEndian.Uint16(b[44+(i-1)*2:]))
		v := int16(binary.LittleEndian.Uint16(b[44+i*2:]))
		if a <= 0 && v > 0 {
			cycles++
		}
	}
	if math.Abs(float64(cycles)-880*7678/16000) > 2 {
		t.Fatal("incorrect pitch", cycles)
	}
	maxResult := callOK(t, "make_music_sequence", `{"bpm":30,"notes":[{"pitch":"48","beats":4},{"pitch":"96","beats":4}]}`)
	closeTo(t, maxResult["duration_seconds"], 16)
}

func TestSortingTraces(t *testing.T) {
	for _, algorithm := range []string{"bubble", "insertion", "selection"} {
		for _, values := range [][]float64{{3, 1, 2}, {-1, -3, 0, -1}, {0, 0}, {1, 2, 3}, {3, 2, 1}} {
			frames := sortingTrace(algorithm, values)
			last := frames[len(frames)-1]
			want := append([]float64(nil), values...)
			sort.Float64s(want)
			for i, v := range want {
				if last.Values[i] != v {
					t.Fatalf("%s: %v", algorithm, last)
				}
			}
			for i, f := range frames {
				if len(f.Values) != len(values) {
					t.Fatal("frame size")
				}
				for _, active := range f.Active {
					if active < 0 || active >= len(values) {
						t.Fatal("active index")
					}
				}
				if i > 0 && (f.Comparisons < frames[i-1].Comparisons || f.Writes < frames[i-1].Writes) {
					t.Fatal("counter decreased")
				}
			}
		}
	}
	s := callOK(t, "make_sorting_lab", `{"algorithm":"bubble","values":[3,2,1]}`)
	closeTo(t, s["comparisons"], 3)
	closeTo(t, s["writes"], 6)
	values := make([]float64, 32)
	for i := range values {
		values[i] = 1e12 - float64(i)*.123456789
	}
	raw, _ := json.Marshal(map[string]any{"algorithm": "bubble", "values": values})
	callOK(t, "make_sorting_lab", string(raw))
}

func TestProjectCriticalPathAndCSV(t *testing.T) {
	d, _ := Lookup("plan_project")
	s := callOK(t, d.Name, string(d.Example))
	closeTo(t, s["duration_minutes"], 105)
	closeTo(t, s["total_work_minutes"], 125)
	rows := s["schedule"].([]any)
	for _, r := range rows {
		row := r.(map[string]any)
		if row["id"] == "docs" {
			closeTo(t, row["slack_minutes"], 40)
			closeTo(t, row["latest_finish_minutes"], 90)
			if row["critical"] != false {
				t.Fatal(row)
			}
		}
	}
	s = callOK(t, "plan_project", `{"tasks":[{"id":"a","title":"=HYPERLINK(\"bad\")","duration_minutes":1,"depends_on":[]},{"id":"b","title":"B","duration_minutes":3,"depends_on":[]}]}`)
	closeTo(t, s["duration_minutes"], 3)
	rows = s["schedule"].([]any)
	closeTo(t, rows[0].(map[string]any)["slack_minutes"], 2)
	records, err := csv.NewReader(strings.NewReader(s["csv"].(string))).ReadAll()
	if err != nil || records[1][1] != `'=HYPERLINK("bad")` {
		t.Fatal(records, err)
	}
}

func TestAppInjectionAndCSP(t *testing.T) {
	attack := `</script><script>globalThis.PWNED=1</script><img src=x onerror=alert(1)>`
	for _, name := range []string{"make_study_app", "make_data_dashboard"} {
		d, _ := Lookup(name)
		var input map[string]any
		_ = json.Unmarshal(d.Example, &input)
		input["title"] = attack
		if name == "make_study_app" {
			input["questions"].([]any)[0].(map[string]any)["prompt"] = attack
		} else {
			input["labels"].([]any)[0] = attack
		}
		raw, _ := json.Marshal(input)
		s := callOK(t, name, string(raw))
		page := s["html"].(string)
		if strings.Count(page, "<script>") != 1 || strings.Count(page, "</script>") != 1 || strings.Contains(page, "<img src=x") || strings.Contains(page, `unsafe-eval`) {
			t.Fatal("unsafe embedding")
		}
		code := strings.Split(strings.Split(page, "<script>")[1], "</script>")[0]
		hash := sha256.Sum256([]byte(code))
		if !strings.Contains(page, base64.StdEncoding.EncodeToString(hash[:])) {
			t.Fatal("CSP hash mismatch")
		}
		if !strings.Contains(page, "connect-src &#39;none&#39;") {
			t.Fatal("missing network restriction")
		}
	}
}

func FuzzSortingTrace(f *testing.F) {
	f.Add([]byte{5, 2, 4, 1, 3})
	f.Add([]byte{0, 255, 0, 255})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) < 2 {
			return
		}
		if len(b) > 32 {
			b = b[:32]
		}
		a := make([]float64, len(b))
		for i, v := range b {
			a[i] = float64(int(v) - 128)
		}
		want := append([]float64(nil), a...)
		sort.Float64s(want)
		for _, algorithm := range []string{"bubble", "insertion", "selection"} {
			frames := sortingTrace(algorithm, a)
			if len(frames) > 1100 {
				t.Fatal("unbounded trace")
			}
			for i, v := range frames[len(frames)-1].Values {
				if v != want[i] {
					t.Fatal("incorrect sort")
				}
			}
		}
	})
}

func FuzzProjectSchedule(f *testing.F) {
	f.Add([]byte{5, 3, 8, 1, 2, 4, 7, 9})
	f.Add([]byte{0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) < 2 {
			return
		}
		n := 2 + int(b[0])%7
		tasks := make([]projectTask, n)
		for i := range tasks {
			tasks[i] = projectTask{ID: strconv.Itoa(i), Title: "Task " + strconv.Itoa(i), Duration: float64(1 + int(b[(i+1)%len(b)])%20), Depends: []string{}}
			for j := 0; j < i; j++ {
				if b[(i+j)%len(b)]%3 == 0 {
					tasks[i].Depends = append(tasks[i].Depends, strconv.Itoa(j))
				}
			}
		}
		// Enumerate dependency/successor paths independently of the schedule's
		// topological and latest-finish passes. Tiny DAGs keep this bounded.
		var before, after func(int) int
		before = func(i int) int {
			best := 0
			for _, id := range tasks[i].Depends {
				j, _ := strconv.Atoi(id)
				best = max(best, before(j))
			}
			return best + int(tasks[i].Duration)
		}
		after = func(i int) int {
			best := 0
			for j := i + 1; j < n; j++ {
				for _, id := range tasks[j].Depends {
					if id == tasks[i].ID {
						best = max(best, after(j))
					}
				}
			}
			return int(tasks[i].Duration) + best
		}
		duration := 0
		for i := range tasks {
			duration = max(duration, before(i))
		}
		raw, _ := json.Marshal(map[string]any{"tasks": tasks})
		result := callOK(t, "plan_project", string(raw))
		closeTo(t, result["duration_minutes"], float64(duration))
		for _, r := range result["schedule"].([]any) {
			row := r.(map[string]any)
			i, _ := strconv.Atoi(row["id"].(string))
			closeTo(t, row["earliest_finish_minutes"], float64(before(i)))
			slack := duration - before(i) - after(i) + int(tasks[i].Duration)
			closeTo(t, row["slack_minutes"], float64(slack))
			if row["critical"] != (slack == 0) {
				t.Fatal("incorrect critical task")
			}
		}
	})
}
