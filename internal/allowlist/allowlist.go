// Package allowlist is the only set of VectorCraft tools a caller may invoke.
package allowlist

import (
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"strings"
)

const (
	maxDepth  = 12
	maxFields = 64
	maxArray  = 4096
	maxString = 8000
)

// ErrRejected means the tool or its arguments are not safe to forward.
var ErrRejected = errors.New("rejected")

var tools = map[string]struct{}{
	"draw_shape":       {},
	"draw_path":        {},
	"add_text":         {},
	"set_paint":        {},
	"create_graph":     {},
	"apply_effect":     {},
	"pathfinder":       {},
	"transform":        {},
	"select_tool":      {},
	"inspect_document": {},
	"list_commands":    {},
	"undo":             {},
	"redo":             {},
	"compose_poster":   {},
}

// Names returns the allowlist in stable order.
func Names() []string {
	out := make([]string, 0, len(tools))
	for _, name := range []string{
		"compose_poster",
		"add_text",
		"apply_effect",
		"create_graph",
		"draw_path",
		"draw_shape",
		"inspect_document",
		"list_commands",
		"pathfinder",
		"redo",
		"select_tool",
		"set_paint",
		"transform",
		"undo",
	} {
		out = append(out, name)
	}
	return out
}

// Allowed reports whether name is a drawing tool this server will forward.
func Allowed(name string) bool {
	_, ok := tools[name]
	return ok
}

// Validate checks a tool name and its JSON arguments.
// Arguments must be a JSON object. Strings may not be absolute paths or contain "..".
func Validate(tool string, arguments json.RawMessage) error {
	if !Allowed(tool) {
		return ErrRejected
	}
	if len(arguments) == 0 || string(arguments) == "null" {
		arguments = []byte("{}")
	}
	var v any
	if err := json.Unmarshal(arguments, &v); err != nil {
		return ErrRejected
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return ErrRejected
	}
	if err := walk(obj, 0); err != nil {
		return ErrRejected
	}
	return nil
}

func walk(v any, depth int) error {
	if depth > maxDepth {
		return errors.New("deep")
	}
	switch t := v.(type) {
	case map[string]any:
		if len(t) > maxFields {
			return errors.New("fields")
		}
		for key, child := range t {
			if err := checkString(key); err != nil {
				return err
			}
			if err := walk(child, depth+1); err != nil {
				return err
			}
		}
	case []any:
		if len(t) > maxArray {
			return errors.New("array")
		}
		for _, child := range t {
			if err := walk(child, depth+1); err != nil {
				return err
			}
		}
	case string:
		return checkString(t)
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return errors.New("number")
		}
	case bool, nil:
		return nil
	default:
		return errors.New("type")
	}
	return nil
}

func checkString(s string) error {
	if len(s) > maxString {
		return errors.New("long")
	}
	if strings.ContainsRune(s, 0) || strings.Contains(s, `\`) || strings.Contains(s, "..") {
		return errors.New("string")
	}
	if filepath.IsAbs(s) {
		return errors.New("abs")
	}
	return nil
}
