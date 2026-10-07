package tools

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed assets/common.js
var appCommonJS string

//go:embed assets/app.css
var appCSS string

//go:embed assets/study.js
var studyJS string

//go:embed assets/dashboard.js
var dashboardJS string

//go:embed assets/sorting.js
var sortingJS string

type studyQuestion struct {
	Prompt      string   `json:"prompt"`
	Choices     []string `json:"choices"`
	Correct     float64  `json:"correct_index"`
	Explanation string   `json:"explanation"`
}

func makeStudyApp(raw json.RawMessage) (any, error) {
	var in struct {
		Title     string          `json:"title"`
		Questions []studyQuestion `json:"questions"`
	}
	decode(raw, &in)
	if err := nonblank(in.Title, "arguments.title"); err != nil {
		return nil, err
	}
	for i, q := range in.Questions {
		field := fmt.Sprintf("arguments.questions[%d]", i)
		if err := nonblank(q.Prompt, field+".prompt"); err != nil {
			return nil, err
		}
		if int(q.Correct) >= len(q.Choices) {
			return nil, invalid(field+".correct_index", "correct_index must identify an existing choice (zero-based)")
		}
		seen := map[string]bool{}
		for _, c := range q.Choices {
			if err := nonblank(c, field+".choices"); err != nil {
				return nil, err
			}
			normalized := strings.ToLower(strings.Join(strings.Fields(c), " "))
			if seen[normalized] {
				return nil, invalid(field+".choices", "choices must be distinct after case/whitespace normalization")
			}
			seen[normalized] = true
		}
	}
	return appResult(in.Title, "study-app", in, `<p class="muted">Practice supplied questions, see explanations, and retry what you missed. Answers are included in this file; this is practice, not a secure exam.</p><div class="toolbar"><button id="all">All questions</button><button id="missed">Retry missed</button><button id="export">Export progress</button><button id="clear">Clear progress</button></div><p id="storage" class="muted" role="status"></p><section class="card"><p id="position" class="muted"></p><h2 id="prompt" tabindex="-1"></h2><div id="choices" class="choices"></div><div id="feedback" aria-live="polite"></div><button id="next" hidden>Next question</button></section><p id="summary" aria-live="polite"></p>`, studyJS)
}

func makeDataDashboard(raw json.RawMessage) (any, error) {
	var in struct {
		Title  string   `json:"title"`
		Labels []string `json:"labels"`
		Series []struct {
			Name   string    `json:"name"`
			Values []float64 `json:"values"`
		} `json:"series"`
	}
	decode(raw, &in)
	if err := nonblank(in.Title, "arguments.title"); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, l := range in.Labels {
		if err := nonblank(l, "arguments.labels"); err != nil {
			return nil, err
		}
		if seen[l] {
			return nil, invalid("arguments.labels", "category labels must be unique")
		}
		seen[l] = true
	}
	seen = map[string]bool{}
	for _, s := range in.Series {
		if err := nonblank(s.Name, "arguments.series.name"); err != nil {
			return nil, err
		}
		if seen[s.Name] {
			return nil, invalid("arguments.series.name", "series names must be unique")
		}
		seen[s.Name] = true
		if len(s.Values) != len(in.Labels) {
			return nil, invalid("arguments.series.values", "every series must have one value per label")
		}
	}
	return appResult(in.Title, "data-dashboard", in, `<p class="muted">Toggle series, compare values, and keep a copy of your data. Categories use the supplied order.</p><div class="toolbar"><label>Chart <select id="chart-type"><option value="line">Line</option><option value="bar">Bars</option></select></label><button id="export">Export visible CSV</button></div><fieldset id="series"><legend>Visible series</legend></fieldset><p id="empty" role="status" hidden>Select a series to view data.</p><section class="card"><svg id="chart" viewBox="0 0 800 360" role="img" aria-label="Data chart; exact values follow in the table"></svg></section><div id="stats" class="stats"></div><div class="table-scroll"><table id="table"><caption>Exact values for visible series</caption></table></div>`, dashboardJS)
}

type sortFrame struct {
	Values      []float64 `json:"values"`
	Active      []int     `json:"active"`
	Action      string    `json:"action"`
	Comparisons int       `json:"comparisons"`
	Writes      int       `json:"writes"`
}

func sortingTrace(algorithm string, values []float64) []sortFrame {
	a := append([]float64(nil), values...)
	frames := make([]sortFrame, 0)
	comparisons, writes := 0, 0
	add := func(action string, active ...int) {
		frames = append(frames, sortFrame{append([]float64(nil), a...), append([]int{}, active...), action, comparisons, writes})
	}
	add("Initial values")
	switch algorithm {
	case "bubble":
		for end := len(a) - 1; end > 0; end-- {
			swapped := false
			for j := 0; j < end; j++ {
				comparisons++
				add("Compare neighbors", j, j+1)
				if a[j] > a[j+1] {
					a[j], a[j+1] = a[j+1], a[j]
					writes += 2
					swapped = true
					add("Swap neighbors", j, j+1)
				}
			}
			if !swapped {
				break
			}
		}
	case "insertion":
		for i := 1; i < len(a); i++ {
			key := a[i]
			j := i - 1
			for j >= 0 {
				comparisons++
				add("Compare with held value "+fmt.Sprint(key), j)
				if a[j] <= key {
					break
				}
				a[j+1] = a[j]
				writes++
				add("Shift right; held value "+fmt.Sprint(key), j, j+1)
				j--
			}
			a[j+1] = key
			writes++
			add("Insert held value", j+1)
		}
	case "selection":
		for i := 0; i < len(a)-1; i++ {
			smallest := i
			for j := i + 1; j < len(a); j++ {
				comparisons++
				add("Compare candidate minimum", smallest, j)
				if a[j] < a[smallest] {
					smallest = j
				}
			}
			if smallest != i {
				a[i], a[smallest] = a[smallest], a[i]
				writes += 2
				add("Place minimum", i, smallest)
			}
		}
	}
	add("Sorted")
	return frames
}

func makeSortingLab(raw json.RawMessage) (any, error) {
	var in struct {
		Algorithm string    `json:"algorithm"`
		Values    []float64 `json:"values"`
	}
	decode(raw, &in)
	frames := sortingTrace(in.Algorithm, in.Values)
	last := frames[len(frames)-1]
	data := map[string]any{"algorithm": in.Algorithm, "frames": frames}
	app, err := appResult("Sorting lab: "+in.Algorithm, "sorting-lab", data, `<p class="muted">Play the actual algorithm trace or inspect one operation at a time. Writes count assignments to array slots; held values are described during insertion sort.</p><div class="toolbar"><button id="play">Play</button><button id="back">Back</button><button id="step">Step</button><button id="reset">Reset</button><label>Delay <input id="speed" type="range" min="50" max="1000" value="350" step="50"> <span id="speed-label">350 ms</span></label></div><section class="card"><p id="position" aria-live="polite"></p><svg id="chart" viewBox="0 0 800 360" role="img" aria-label="Sorting state; exact array values follow"></svg><p id="array" class="mono"></p><p id="counts"></p></section>`, sortingJS)
	if err != nil {
		return nil, err
	}
	app["sorted_values"] = last.Values
	app["comparisons"] = last.Comparisons
	app["writes"] = last.Writes
	app["frame_count"] = len(frames)
	return app, nil
}
