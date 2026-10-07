// Package tools provides bounded, deterministic utilities without model APIs,
// subprocesses, network requests, or persistent user data.
package tools

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// Error is safe to return to an agent. Field identifies what to correct.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

func (e *Error) Error() string { return e.Message }
func invalid(field, message string) *Error {
	return &Error{Code: "invalid_arguments", Message: message, Field: field}
}

// Definition is a function-calling descriptor plus a short working example.
// InputSchema uses the JSON Schema subset enforced by validate below.
type Definition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Category    string          `json:"category"`
	InputSchema map[string]any  `json:"input_schema"`
	Example     json.RawMessage `json:"example"`
}

type entry struct {
	Definition
	run func(json.RawMessage) (any, error)
}

func str(max int) map[string]any {
	return map[string]any{"type": "string", "maxLength": max, "x-maxBytes": max}
}
func number() map[string]any {
	return map[string]any{"type": "number", "minimum": -1e12, "maximum": 1e12}
}
func choice(values ...string) map[string]any { return map[string]any{"type": "string", "enum": values} }
func array(item map[string]any, min, max int) map[string]any {
	return map[string]any{"type": "array", "items": item, "minItems": min, "maxItems": max}
}
func object(properties map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

var registry = []entry{
	{Definition{"calculate", "Evaluate arithmetic with parentheses, + - * / % and ^ (or **). Functions: sqrt, abs, ln, log10, sin, cos, tan, exp, floor, ceil, round, pow, min, max. Constants pi and e. Trigonometry uses radians. Approximate float64 results; no symbolic algebra or code execution.", "math", object(map[string]any{"expression": str(512)}, "expression"), json.RawMessage(`{"expression":"(85 + 90 + 95) / 3"}`)}, calculate},
	{Definition{"convert_units", "Convert length, mass, duration, volume, speed, area, or temperature. Use exact unit identifiers from the schema. gal and fl_oz are US units; C/F/K are absolute temperatures. No currency rates.", "math", object(map[string]any{"value": number(), "from": choice(unitNames...), "to": choice(unitNames...)}, "value", "from", "to"), json.RawMessage(`{"value":72,"from":"F","to":"C"}`)}, convertUnits},
	{Definition{"summarize_numbers", "Compute count, sum, mean, median, min, max, range, population and sample standard deviation for 1 to 4096 numbers. Sample deviation is null for one value. Does not infer causes or significance.", "data", object(map[string]any{"values": array(number(), 1, 4096)}, "values"), json.RawMessage(`{"values":[85,90,95]}`)}, summarizeNumbers},
	{Definition{"date_math", "Add signed calendar days to a date or find signed days between two dates. Use YYYY-MM-DD. Supply exactly one of days or end. Dates use the proleptic Gregorian calendar, without time zones, times, or holiday rules.", "planning", object(map[string]any{"start": str(10), "end": str(10), "days": map[string]any{"type": "integer", "minimum": -365000, "maximum": 365000}}, "start"), json.RawMessage(`{"start":"2026-10-07","days":30}`)}, dateMath},
	{Definition{"inspect_csv", "Inspect a CSV string with a header row. Return row count, up to 5 preview rows, and per-column missing/numeric counts and statistics. Limits: 1000 rows, 32 columns, 128 KiB. Quoted commas and newlines work. No files, URLs, or inferred conclusions.", "data", object(map[string]any{"csv": str(128 * 1024)}, "csv"), json.RawMessage(`{"csv":"subject,hours\nC,4\nPython,7\nLua,3"}`)}, inspectCSV},
	{Definition{"analyze_text", "Count Unicode characters, whitespace-delimited words, lines, and estimated reading time at 200 words per minute. Return up to 10 frequent lowercased words. No rewriting, grammar judgment, or AI text detection.", "writing", object(map[string]any{"text": str(64 * 1024)}, "text"), json.RawMessage(`{"text":"Make your next idea real. Learn, write, and build."}`)}, analyzeText},
	{Definition{"make_diagram", "Create a simple SVG flowchart from 1 to 12 labeled nodes and up to 24 directed edges. Nodes are arranged top to bottom in supplied order. Use short labels. Return SVG text, media type, and filename. No renderer needed; no scripts, HTML, links, or remote assets.", "visuals", object(map[string]any{
		"title": str(80),
		"nodes": array(object(map[string]any{"id": str(32), "label": str(60)}, "id", "label"), 1, 12),
		"edges": array(object(map[string]any{"from": str(32), "to": str(32), "label": str(24)}, "from", "to"), 0, 24),
	}, "title", "nodes"), json.RawMessage(`{"title":"App request","nodes":[{"id":"app","label":"Edsger"},{"id":"server","label":"Tool server"},{"id":"result","label":"Verified result"}],"edges":[{"from":"app","to":"server"},{"from":"server","to":"result"}]}`)}, makeDiagram},
}

// Catalog returns tools in stable order. An empty category selects all tools.
func Catalog(category string) []Definition {
	out := make([]Definition, 0, len(registry))
	for _, e := range registry {
		if category == "" || category == e.Category {
			out = append(out, e.Definition)
		}
	}
	return out
}

// Lookup finds a descriptor without requiring a renderer or session.
func Lookup(name string) (Definition, bool) {
	for _, e := range registry {
		if e.Name == name {
			return e.Definition, true
		}
	}
	return Definition{}, false
}

// Call validates against the advertised schema before running a utility.
func Call(name string, raw json.RawMessage) (any, error) {
	for _, e := range registry {
		if e.Name != name {
			continue
		}
		if len(raw) > 256*1024 {
			return nil, invalid("arguments", "arguments exceed 256 KiB")
		}
		if !utf8.Valid(raw) {
			return nil, invalid("arguments", "JSON must be valid UTF-8")
		}
		value, err := parseJSON(raw)
		if err != nil {
			return nil, err
		}
		if err := validate(e.InputSchema, value, "arguments"); err != nil {
			return nil, err
		}
		// Canonicalize the validated object before decoding typed arguments.
		normalized, err := json.Marshal(value)
		if err != nil {
			return nil, invalid("arguments", "invalid JSON value")
		}
		result, err := e.run(normalized)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return nil, &Error{Code: "numeric_range", Message: "result cannot be represented as finite JSON; reduce the numeric range"}
		}
		if len(encoded) > 1<<20 {
			return nil, &Error{Code: "result_too_large", Message: "result exceeds 1 MiB; use a smaller input"}
		}
		return result, nil
	}
	return nil, &Error{Code: "unknown_tool", Message: "unknown tool; choose a name from GET /v1/tools", Field: "tool"}
}

// validate intentionally supports only the schema keywords emitted above.
func validate(schema map[string]any, value any, path string) error {
	switch schema["type"] {
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return invalid(path, "expected an object")
		}
		props := schema["properties"].(map[string]any)
		for _, key := range schema["required"].([]string) {
			if _, ok := obj[key]; !ok {
				return invalid(path+"."+key, "required field is missing")
			}
		}
		for key, v := range obj {
			s, ok := props[key]
			if !ok {
				return invalid(path+"."+key, "unknown field; use the tool schema")
			}
			if err := validate(s.(map[string]any), v, path+"."+key); err != nil {
				return err
			}
		}
	case "string":
		v, ok := value.(string)
		if !ok {
			return invalid(path, "expected a string")
		}
		if !utf8.ValidString(v) || strings.ContainsRune(v, 0) {
			return invalid(path, "text must be valid Unicode without null characters")
		}
		if min, ok := schema["minLength"].(int); ok && utf8.RuneCountInString(v) < min {
			return invalid(path, "text is too short")
		}
		if max, ok := schema["maxLength"].(int); ok && utf8.RuneCountInString(v) > max {
			return invalid(path, fmt.Sprintf("text exceeds %d Unicode characters", max))
		}
		if max, ok := schema["x-maxBytes"].(int); ok && len(v) > max {
			return invalid(path, fmt.Sprintf("text exceeds %d UTF-8 bytes", max))
		}
		if options, ok := schema["enum"].([]string); ok {
			found := false
			for _, option := range options {
				if v == option {
					found = true
					break
				}
			}
			if !found {
				return invalid(path, "choose a value from the schema enum")
			}
		}
	case "number", "integer":
		n, ok := value.(json.Number)
		if !ok {
			return invalid(path, "expected a JSON number")
		}
		v, err := n.Float64()
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return invalid(path, "expected a finite number")
		}
		if schema["type"] == "integer" && v != math.Trunc(v) {
			return invalid(path, "expected an integer")
		}
		if v < asFloat(schema["minimum"]) || v > asFloat(schema["maximum"]) {
			return invalid(path, "number is outside the schema limits")
		}
	case "array":
		v, ok := value.([]any)
		if !ok {
			return invalid(path, "expected an array")
		}
		if len(v) < schema["minItems"].(int) || len(v) > schema["maxItems"].(int) {
			return invalid(path, "array length is outside the schema limits")
		}
		for i, item := range v {
			if err := validate(schema["items"].(map[string]any), item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return invalid(path, "expected a boolean")
		}
	}
	return nil
}

func asFloat(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case float64:
		return n
	}
	panic("invalid internal numeric schema")
}

func decode(raw json.RawMessage, into any) { _ = json.Unmarshal(raw, into) } // validated by Call
