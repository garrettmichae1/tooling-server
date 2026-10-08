package tools

import (
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"io"
	"math"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func callOK(t *testing.T, name, raw string) map[string]any {
	t.Helper()
	result, err := Call(name, []byte(raw))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func closeTo(t *testing.T, got any, want float64) {
	t.Helper()
	v, ok := got.(float64)
	if !ok || math.Abs(v-want) > 1e-10*math.Max(1, math.Abs(want)) {
		t.Fatalf("got %v, want %g", got, want)
	}
}

func TestEveryCatalogExample(t *testing.T) {
	seen := map[string]bool{}
	for _, definition := range Catalog("") {
		t.Run(definition.Name, func(t *testing.T) {
			if seen[definition.Name] {
				t.Fatal("duplicate tool name")
			}
			seen[definition.Name] = true
			if definition.Description == "" || definition.Category == "" {
				t.Fatal("missing metadata")
			}
			if _, ok := Lookup(definition.Name); !ok {
				t.Fatal("lookup missing")
			}
			callOK(t, definition.Name, string(definition.Example))
		})
	}
	if len(seen) != 39 {
		t.Fatalf("unexpected tool count %d", len(seen))
	}
	for _, definition := range Catalog("math") {
		if definition.Category != "math" {
			t.Fatal("bad category filter")
		}
	}
	if len(Catalog("nonexistent")) != 0 {
		t.Fatal("unknown category returned tools")
	}
}

func TestArithmetic(t *testing.T) {
	cases := []struct {
		expr string
		want float64
	}{
		{"2 + 3 * 4", 14}, {"(2 + 3) * 4", 20}, {"2^3^2", 512}, {"-2^2", -4}, {"(-2)^2", 4}, {"2^-2", .25}, {"2 ** 3", 8}, {"7 % 3", 1}, {"1e-3 + .5", .501},
		{"sqrt(49)", 7}, {"abs(-3)", 3}, {"ln(e)", 1}, {"log10(100)", 2}, {"sin(pi/2)", 1}, {"cos(0)", 1}, {"tan(0)", 0}, {"exp(0)", 1}, {"floor(2.9)", 2}, {"ceil(2.1)", 3}, {"round(-2.5)", -3}, {"pow(2,3)", 8}, {"min(3,2)", 2}, {"max(3,2)", 3}, {"sqrt(pow(3,2)+pow(4,2))", 5},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]string{"expression": tc.expr})
			closeTo(t, callOK(t, "calculate", string(raw))["value"], tc.want)
		})
	}
}

func TestInvalidArguments(t *testing.T) {
	cases := []struct{ name, raw string }{
		{"missing", "{}"}, {"calculate", "null"}, {"calculate", "[]"}, {"calculate", `{}`}, {"calculate", `{"expression":null}`}, {"calculate", `{"expression":"2","extra":1}`}, {"calculate", `{"expression":"2"} {}`}, {"calculate", `{"expression":"1","expression":"2"}`},
		{"calculate", `{"expression":"1/0"}`}, {"calculate", `{"expression":"1%0"}`}, {"calculate", `{"expression":"sqrt(-1)"}`}, {"calculate", `{"expression":"ln(0)"}`}, {"calculate", `{"expression":"exp(1000)"}`}, {"calculate", `{"expression":"2 3"}`}, {"calculate", `{"expression":"2^"}`}, {"calculate", `{"expression":"(2"}`}, {"calculate", `{"expression":"os.system(1)"}`}, {"calculate", `{"expression":"sqrt(1,2)"}`}, {"calculate", `{"expression":"pow(1)"}`}, {"calculate", `{"expression":"foo(2)"}`}, {"calculate", `{"expression":"."}`}, {"calculate", `{"expression":""}`},
		{"convert_units", `{"value":3,"from":"m","to":"kg"}`}, {"convert_units", `{"value":-1,"from":"K","to":"C"}`}, {"convert_units", `{"value":1,"from":"bad","to":"m"}`}, {"convert_units", `{"value":"1","from":"m","to":"cm"}`},
		{"summarize_numbers", `{"values":[]}`}, {"summarize_numbers", `{"values":[1e13]}`}, {"summarize_numbers", `{"values":[null]}`},
		{"date_math", `{"start":"2026-02-30","days":1}`}, {"date_math", `{"start":"2026-01-01"}`}, {"date_math", `{"start":"2026-01-01","days":1,"end":"2026-01-02"}`}, {"date_math", `{"start":"2026-01-01","days":1.5}`}, {"date_math", `{"start":"9999-12-31","days":1}`},
		{"inspect_csv", `{"csv":"a,b\n1"}`}, {"inspect_csv", `{"csv":"a,a\n1,2"}`}, {"inspect_csv", `{"csv":"a,\n1,2"}`}, {"inspect_csv", `{"csv":"a\n\"unterminated"}`},
		{"make_diagram", `{"title":"x","nodes":[]}`}, {"make_diagram", `{"title":"x","nodes":[{"id":"a","label":"A"},{"id":"a","label":"B"}]}`}, {"make_diagram", `{"title":"x","nodes":[{"id":"a","label":"A"}],"edges":[{"from":"a","to":"b"}]}`}, {"make_diagram", `{"title":"x","nodes":[{"id":"a","label":"A"}],"edges":[{"from":"a","to":"a"}]}`},
		{"solve_quadratic", `{"a":0,"b":0}`}, {"linear_regression", `{"points":[[1,1],[1,2]]}`}, {"linear_regression", `{"points":[[1,2,3],[2,4]]}`},
		{"solve_linear_system", `{"matrix":[[1,2],[3]],"constants":[1,2]}`}, {"solve_linear_system", `{"matrix":[[1]],"constants":[1,2]}`}, {"solve_linear_system", `{"matrix":[[1.5]],"constants":[1]}`},
		{"combinatorics", `{"operation":"factorial","n":2,"k":1}`}, {"combinatorics", `{"operation":"combinations","n":2,"k":3}`}, {"combinatorics", `{"operation":"permutations","n":2}`}, {"combinatorics", `{"operation":"factorial","n":-1}`},
		{"number_theory", `{"operation":"factorize","a":1}`}, {"number_theory", `{"operation":"gcd","a":2}`}, {"number_theory", `{"operation":"factorize","a":2,"b":3}`},
		{"convert_base", `{"value":"2","from":"2","to":"10"}`}, {"convert_base", `{"value":"0xff","from":"16","to":"10"}`}, {"convert_base", `{"value":"1_0","from":"10","to":"2"}`},
		{"inspect_json", `{"json":"{\"a\":1,\"a\":2}"}`}, {"inspect_json", `{"json":"{}[]"}`}, {"inspect_json", `{"json":"{"}`},
		{"test_regex", `{"pattern":"(?=x)","text":"x"}`}, {"test_regex", `{"pattern":"[","text":"x"}`},
		{"encode_text", `{"operation":"decode","format":"base64","text":"!"}`}, {"encode_text", `{"operation":"decode","format":"hex","text":"ff"}`}, {"encode_text", `{"operation":"decode","format":"hex","text":"00"}`}, {"encode_text", `{"operation":"decode","format":"url_query","text":"%xx"}`},
		{"grade_quiz", `{"answers":["a"],"key":["a","b"]}`}, {"grade_quiz", `{"answers":["a"],"key":[""]}`}, {"make_flashcards", `{"title":"x","cards":[{"question":"","answer":"a"}]}`},
		{"make_calendar_event", `{"title":"x","start":"2026-10-07T18:00:00","end":"2026-10-07T19:00:00Z"}`}, {"make_calendar_event", `{"title":"x","start":"2026-10-07T18:00:00Z","end":"2026-10-07T18:00:00Z"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/"+tc.raw, func(t *testing.T) {
			if _, err := Call(tc.name, []byte(tc.raw)); err == nil {
				t.Fatal("invalid arguments accepted")
			}
		})
	}
	long := "(" + strings.Repeat("(", 40) + "1" + strings.Repeat(")", 41)
	raw, _ := json.Marshal(map[string]string{"expression": long})
	if _, err := Call("calculate", raw); err == nil {
		t.Fatal("deep expression accepted")
	}
	deep := strings.Repeat("[", 40) + "1" + strings.Repeat("]", 40)
	raw, _ = json.Marshal(map[string]string{"json": deep})
	if _, err := Call("inspect_json", raw); err == nil {
		t.Fatal("deep JSON accepted")
	}
	if _, err := Call("calculate", []byte{'{', '"', 0xff, '"', ':', '1', '}'}); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
}

func TestUnitConversionsAndRoundTrips(t *testing.T) {
	closeTo(t, callOK(t, "convert_units", `{"value":32,"from":"F","to":"C"}`)["value"], 0)
	closeTo(t, callOK(t, "convert_units", `{"value":1,"from":"mi","to":"m"}`)["value"], 1609.344)
	closeTo(t, callOK(t, "convert_units", `{"value":1,"from":"gal","to":"L"}`)["value"], 3.785411784)
	for from, a := range units {
		for to, b := range units {
			if a.dimension != b.dimension {
				continue
			}
			t.Run(from+"/"+to, func(t *testing.T) {
				value := 123.456
				raw, _ := json.Marshal(map[string]any{"value": value, "from": from, "to": to})
				first := callOK(t, "convert_units", string(raw))["value"]
				raw, _ = json.Marshal(map[string]any{"value": first, "from": to, "to": from})
				closeTo(t, callOK(t, "convert_units", string(raw))["value"], value)
			})
		}
	}
}

func TestStatsAndCSV(t *testing.T) {
	s := callOK(t, "summarize_numbers", `{"values":[5,1,4,2]}`)
	closeTo(t, s["mean"], 3)
	closeTo(t, s["median"], 3)
	closeTo(t, s["sum"], 12)
	closeTo(t, s["population_stddev"], math.Sqrt(2.5))
	closeTo(t, s["sample_stddev"], math.Sqrt(10.0/3))
	s = callOK(t, "summarize_numbers", `{"values":[7]}`)
	if s["sample_stddev"] != nil {
		t.Fatal("sample deviation for one observation must be null")
	}
	s = callOK(t, "inspect_csv", `{"csv":"name,score\n\"A, B\",85\n\"two\nlines\",\nC,oops\nD,95"}`)
	closeTo(t, s["rows"], 4)
	columns := s["columns"].([]any)
	score := columns[1].(map[string]any)
	closeTo(t, score["numeric"], 2)
	closeTo(t, score["missing"], 1)
	closeTo(t, score["non_numeric"], 1)
	closeTo(t, score["statistics"].(map[string]any)["mean"], 90)
	s = callOK(t, "linear_regression", `{"points":[[1,3],[2,5],[3,7]]}`)
	closeTo(t, s["slope"], 2)
	closeTo(t, s["intercept"], 1)
	closeTo(t, s["r_squared"], 1)
	closeTo(t, s["rmse"], 0)
	s = callOK(t, "linear_regression", `{"points":[[1,4],[2,4]]}`)
	if s["r_squared"] != nil || s["correlation"] != nil {
		t.Fatal("constant y correlation must be undefined")
	}
}

func TestDatesAndText(t *testing.T) {
	s := callOK(t, "date_math", `{"start":"2024-02-28","days":1.0}`)
	if s["end"] != "2024-02-29" {
		t.Fatal(s)
	}
	s = callOK(t, "date_math", `{"start":"2026-03-01","days":-1}`)
	if s["end"] != "2026-02-28" {
		t.Fatal(s)
	}
	s = callOK(t, "date_math", `{"start":"1000-01-01","end":"2000-01-01"}`)
	closeTo(t, s["days"], 365242)
	s = callOK(t, "analyze_text", `{"text":"Hi 👋\r\nHI café"}`)
	closeTo(t, s["words"], 4)
	closeTo(t, s["characters"], 13)
	closeTo(t, s["lines"], 2)
	s = callOK(t, "analyze_text", `{"text":""}`)
	closeTo(t, s["words"], 0)
	closeTo(t, s["lines"], 0)
	s = callOK(t, "compare_text", `{"before":"a\nb","after":"a\nc\nb"}`)
	closeTo(t, s["inserted_lines"], 1)
	closeTo(t, s["deleted_lines"], 0)
	s = callOK(t, "compare_text", `{"before":"","after":"a"}`)
	closeTo(t, s["inserted_lines"], 1)
	closeTo(t, s["deleted_lines"], 0)
	s = callOK(t, "transform_text", `{"operation":"unique_lines","text":"C\nPython\nC\nLua"}`)
	if s["text"] != "C\nPython\nLua" {
		t.Fatal(s)
	}
	s = callOK(t, "transform_text", `{"operation":"normalize_whitespace","text":"  a\n  b\t c "}`)
	if s["text"] != "a b c" {
		t.Fatal(s)
	}
}

func TestExactMath(t *testing.T) {
	s := callOK(t, "solve_linear_system", `{"matrix":[[1,1,1],[2,3,1],[3,4,1]],"constants":[4,16,23]}`)
	if s["status"] != "unique" || s["onto"] != true || s["one_to_one"] != true {
		t.Fatal(s)
	}
	got, _ := json.Marshal(s["solution"])
	if string(got) != `["2","5","-3"]` {
		t.Fatal(string(got))
	}
	s = callOK(t, "solve_linear_system", `{"matrix":[[3]],"constants":[1]}`)
	got, _ = json.Marshal(s["solution"])
	if string(got) != `["1/3"]` {
		t.Fatal(string(got))
	}
	s = callOK(t, "solve_linear_system", `{"matrix":[[1,2],[2,4]],"constants":[1,3]}`)
	if s["status"] != "inconsistent" {
		t.Fatal(s)
	}
	s = callOK(t, "solve_linear_system", `{"matrix":[[1,2],[2,4]],"constants":[1,2]}`)
	if s["status"] != "infinite" {
		t.Fatal(s)
	}
	s = callOK(t, "combinatorics", `{"operation":"factorial","n":20}`)
	if s["value"] != "2432902008176640000" {
		t.Fatal(s)
	}
	s = callOK(t, "combinatorics", `{"operation":"combinations","n":10,"k":3}`)
	if s["value"] != "120" {
		t.Fatal(s)
	}
	s = callOK(t, "combinatorics", `{"operation":"permutations","n":10,"k":3}`)
	if s["value"] != "720" {
		t.Fatal(s)
	}
	s = callOK(t, "combinatorics", `{"operation":"factorial","n":0}`)
	if s["value"] != "1" {
		t.Fatal(s)
	}
	s = callOK(t, "number_theory", `{"operation":"gcd","a":48,"b":18}`)
	if s["value"] != "6" {
		t.Fatal(s)
	}
	s = callOK(t, "number_theory", `{"operation":"lcm","a":48,"b":18}`)
	if s["value"] != "144" {
		t.Fatal(s)
	}
	s = callOK(t, "number_theory", `{"operation":"lcm","a":0,"b":0}`)
	if s["value"] != "0" {
		t.Fatal(s)
	}
	s = callOK(t, "number_theory", `{"operation":"factorize","a":360}`)
	got, _ = json.Marshal(s["factors"])
	if string(got) != `[{"exponent":3,"prime":2},{"exponent":2,"prime":3},{"exponent":1,"prime":5}]` {
		t.Fatal(string(got))
	}
	s = callOK(t, "convert_base", `{"value":"-ff","from":"16","to":"10"}`)
	if s["value"] != "-255" {
		t.Fatal(s)
	}
}

func TestQuadraticBranches(t *testing.T) {
	for _, tc := range []struct{ raw, status string }{{`{"a":1,"b":-5,"c":6}`, "two_real"}, {`{"a":1,"b":2,"c":1}`, "one_real"}, {`{"a":1,"b":0,"c":1}`, "two_complex"}, {`{"a":0,"b":2,"c":-4}`, "one_real"}, {`{"a":0,"b":0,"c":0}`, "all_real"}, {`{"a":0,"b":0,"c":1}`, "none"}} {
		s := callOK(t, "solve_quadratic", tc.raw)
		if s["status"] != tc.status {
			t.Fatal(s)
		}
	}
	s := callOK(t, "solve_quadratic", `{"a":1,"b":1000000000,"c":1}`)
	roots := s["roots"].([]any)
	closeTo(t, roots[1], -1e-9)
}

func TestDeveloperUtilities(t *testing.T) {
	s := callOK(t, "inspect_json", `{"json":"{\"n\":9007199254740993}"}`)
	if !strings.Contains(s["formatted"].(string), "9007199254740993") {
		t.Fatal("integer precision lost")
	}
	s = callOK(t, "test_regex", `{"pattern":"(a)(b)?","text":"a ab"}`)
	matches := s["matches"].([]any)
	groups := matches[0].(map[string]any)["groups"].([]any)
	if groups[1] != nil {
		t.Fatal("unmatched capture must be null")
	}
	for _, format := range []string{"base64", "hex", "url_query"} {
		for _, text := range []string{"", "Hello 👋 + &\n", "café"} {
			raw, _ := json.Marshal(map[string]string{"operation": "encode", "format": format, "text": text})
			s = callOK(t, "encode_text", string(raw))
			raw, _ = json.Marshal(map[string]string{"operation": "decode", "format": format, "text": s["text"].(string)})
			s = callOK(t, "encode_text", string(raw))
			if s["text"] != text {
				t.Fatal("encoding round trip failed")
			}
		}
	}
	s = callOK(t, "hash_text", `{"algorithm":"sha256","text":"hello"}`)
	if s["hex"] != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatal(s)
	}
	s = callOK(t, "hash_text", `{"algorithm":"sha256","text":""}`)
	if s["hex"] != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatal(s)
	}
}

func TestStudyAndArtifacts(t *testing.T) {
	s := callOK(t, "grade_quiz", `{"answers":["  Elasticity ",""],"key":["elasticity","authentication"]}`)
	closeTo(t, s["percentage"], 50)
	s = callOK(t, "make_flashcards", `{"title":"Study","cards":[{"question":"What, exactly?","answer":"One\nTwo"}]}`)
	files := s["files"].([]any)
	reader := csv.NewReader(strings.NewReader(files[0].(map[string]any)["text"].(string)))
	records, err := reader.ReadAll()
	if err != nil || len(records) != 2 || records[1][1] != "One\nTwo" {
		t.Fatal(records, err)
	}
	s = callOK(t, "make_diagram", `{"title":"A & B","nodes":[{"id":"a","label":"<script>alert(1)</script>"}]}`)
	svg := s["svg"].(string)
	if strings.Contains(svg, "<script>") {
		t.Fatal("SVG injection")
	}
	dec := xml.NewDecoder(strings.NewReader(svg))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	s = callOK(t, "make_calendar_event", `{"title":"Study, math; code","start":"2026-10-07T18:00:00-05:00","end":"2026-10-07T19:00:00-05:00","description":"line1\nBEGIN:VEVENT"}`)
	text := s["text"].(string)
	if !strings.Contains(text, "DTSTART:20261007T230000Z\r\n") || !strings.Contains(text, `SUMMARY:Study\, math\; code`) || strings.Count(text, "\r\nBEGIN:VEVENT\r\n") != 1 {
		t.Fatal(text)
	}
	line := "DESCRIPTION:" + strings.Repeat("👋", 80)
	folded := foldCalendarLine(line)
	if strings.ReplaceAll(folded, "\r\n ", "") != line {
		t.Fatal("folding changed text")
	}
	for _, part := range strings.Split(folded, "\r\n") {
		if len(part) > 75 || !utf8.ValidString(part) {
			t.Fatal("invalid fold")
		}
	}
}

func TestParallelCalls(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, def := range Catalog("") {
				if _, err := Call(def.Name, def.Example); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
}

func FuzzToolCalls(f *testing.F) {
	for _, def := range Catalog("") {
		f.Add(def.Name, string(def.Example))
	}
	f.Add("calculate", `{"expression":"1/0"}`)
	f.Add("calculate", `null`)
	f.Fuzz(func(t *testing.T, name, raw string) {
		if len(raw) > 256*1024 {
			return
		}
		result, err := Call(name, []byte(raw))
		if err == nil {
			if _, err := json.Marshal(result); err != nil {
				t.Fatal(err)
			}
		}
	})
}
