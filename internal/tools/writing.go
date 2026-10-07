package tools

import (
	"encoding/json"
	"sort"
	"strings"
)

func lines(text string) []string {
	if text == "" {
		return []string{}
	}
	return strings.Split(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\n")
}

func transformText(raw json.RawMessage) (any, error) {
	var in struct{ Operation, Text string }
	decode(raw, &in)
	result := in.Text
	switch in.Operation {
	case "lowercase":
		result = strings.ToLower(result)
	case "uppercase":
		result = strings.ToUpper(result)
	case "trim":
		result = strings.TrimSpace(result)
	case "normalize_whitespace":
		result = strings.Join(strings.Fields(result), " ")
	case "sort_lines", "unique_lines":
		items := make([]string, 0)
		seen := map[string]bool{}
		for _, line := range lines(result) {
			if strings.TrimSpace(line) == "" {
				continue
			}
			if in.Operation == "unique_lines" && seen[line] {
				continue
			}
			seen[line] = true
			items = append(items, line)
		}
		if in.Operation == "sort_lines" {
			sort.Strings(items)
		}
		result = strings.Join(items, "\n")
	}
	return map[string]any{"text": result, "operation": in.Operation}, nil
}

type diffOperation struct {
	Operation string `json:"operation"`
	Text      string `json:"text"`
}

func compareText(raw json.RawMessage) (any, error) {
	var in struct{ Before, After string }
	decode(raw, &in)
	a, b := lines(in.Before), lines(in.After)
	if len(a) > 200 || len(b) > 200 {
		return nil, invalid("arguments", "text comparison supports at most 200 lines per input")
	}
	// Bounded 201x201 dynamic-programming table, deterministic deletion on ties.
	table := make([][]int, len(a)+1)
	for i := range table {
		table[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else {
				table[i][j] = max(table[i+1][j], table[i][j+1])
			}
		}
	}
	i, j, inserted, deleted := 0, 0, 0, 0
	ops := make([]diffOperation, 0, len(a)+len(b))
	for i < len(a) || j < len(b) {
		if i < len(a) && j < len(b) && a[i] == b[j] {
			ops = append(ops, diffOperation{"equal", a[i]})
			i++
			j++
		} else if i < len(a) && (j == len(b) || table[i+1][j] >= table[i][j+1]) {
			ops = append(ops, diffOperation{"delete", a[i]})
			i++
			deleted++
		} else {
			ops = append(ops, diffOperation{"insert", b[j]})
			j++
			inserted++
		}
	}
	return map[string]any{"equal": inserted == 0 && deleted == 0, "inserted_lines": inserted, "deleted_lines": deleted, "operations": ops}, nil
}
