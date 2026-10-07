package tools

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

type summary struct {
	Count            int      `json:"count"`
	Sum              float64  `json:"sum"`
	Mean             float64  `json:"mean"`
	Median           float64  `json:"median"`
	Min              float64  `json:"min"`
	Max              float64  `json:"max"`
	Range            float64  `json:"range"`
	PopulationStdDev float64  `json:"population_stddev"`
	SampleStdDev     *float64 `json:"sample_stddev"`
}

func summarizeNumbers(raw json.RawMessage) (any, error) {
	var in struct {
		Values []float64 `json:"values"`
	}
	decode(raw, &in)
	return summarize(in.Values), nil
}

func summarize(values []float64) summary {
	// Welford variance avoids cancellation; Kahan sum limits accumulation error.
	scale := 0.0
	for _, x := range values {
		scale = math.Max(scale, math.Abs(x))
	}
	if scale == 0 {
		scale = 1
	}
	var mean, m2, sum, compensation float64
	for i, x := range values {
		normalized := x / scale
		delta := normalized - mean
		mean += delta / float64(i+1)
		m2 += delta * (normalized - mean)
		y := x - compensation
		next := sum + y
		compensation = (next - sum) - y
		sum = next
	}
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	n := len(ordered)
	mid := ordered[n/2]
	if n%2 == 0 {
		mid = (ordered[n/2-1] + mid) / 2
	}
	result := summary{Count: n, Sum: sum, Mean: mean * scale, Median: mid, Min: ordered[0], Max: ordered[n-1], Range: ordered[n-1] - ordered[0], PopulationStdDev: math.Sqrt(math.Max(0, m2)/float64(n)) * scale}
	if n > 1 {
		s := math.Sqrt(math.Max(0, m2)/float64(n-1)) * scale
		result.SampleStdDev = &s
	}
	return result
}

type column struct {
	Name       string   `json:"name"`
	Missing    int      `json:"missing"`
	Numeric    int      `json:"numeric"`
	NonNumeric int      `json:"non_numeric"`
	Statistics *summary `json:"statistics,omitempty"`
}

func inspectCSV(raw json.RawMessage) (any, error) {
	var in struct {
		CSV string `json:"csv"`
	}
	decode(raw, &in)
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(in.CSV, "\ufeff")))
	headers, err := reader.Read()
	if err != nil || len(headers) == 0 || len(headers) > 32 {
		return nil, invalid("arguments.csv", "CSV needs a header row with 1 to 32 columns")
	}
	cols := make([]column, len(headers))
	numbers := make([][]float64, len(headers))
	seen := map[string]bool{}
	for i, header := range headers {
		header = strings.TrimSpace(header)
		if header == "" || len(header) > 120 || seen[header] {
			return nil, invalid("arguments.csv", "headers must be unique, nonempty, and at most 120 bytes")
		}
		seen[header] = true
		headers[i] = header
		cols[i].Name = header
	}
	preview := make([][]string, 0, 5)
	rows := 0
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, invalid("arguments.csv", "invalid CSV quoting or inconsistent column count")
		}
		rows++
		if rows > 1000 {
			return nil, invalid("arguments.csv", "CSV exceeds 1000 data rows")
		}
		for i, cell := range record {
			if len(cell) > 4096 {
				return nil, invalid("arguments.csv", "CSV cell exceeds 4096 UTF-8 bytes")
			}
			cell = strings.TrimSpace(cell)
			if cell == "" {
				cols[i].Missing++
				continue
			}
			v, err := strconv.ParseFloat(cell, 64)
			if err == nil && finite(v) && math.Abs(v) <= 1e12 {
				cols[i].Numeric++
				numbers[i] = append(numbers[i], v)
			} else {
				cols[i].NonNumeric++
			}
		}
		if len(preview) < 5 {
			preview = append(preview, record)
		}
	}
	for i := range cols {
		if len(numbers[i]) > 0 {
			s := summarize(numbers[i])
			cols[i].Statistics = &s
		}
	}
	return map[string]any{"rows": rows, "columns": cols, "headers": headers, "preview": preview, "preview_truncated": rows > len(preview), "numeric_rule": "finite dot-decimal numbers with absolute value at most 1e12; blanks are missing; statistics use numeric cells only"}, nil
}
