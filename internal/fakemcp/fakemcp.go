// Package fakemcp is a stdio stand-in for vectorcraft-cli used by tests.
package fakemcp

import (
	"bufio"
	"encoding/json"
	"image"
	"image/png"
	"os"
)

// Run reads MCP messages from stdin and writes replies to stdout.
func Run() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	enc := json.NewEncoder(os.Stdout)
	nextID := 0
	for sc.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil || req.Method == "" || len(req.ID) == 0 {
			continue
		}
		switch req.Method {
		case "initialize":
			result := map[string]any{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "fake", "version": "0"},
			}
			if os.Getenv("FIGURE_TOKEN") != "" {
				result["leaked"] = true
			}
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		case "tools/call":
			if req.Params.Name == "export" {
				if path, _ := req.Params.Arguments["path"].(string); path != "" {
					writePNG(path)
				}
			}
			nextID++
			result := map[string]any{
				"content": []any{map[string]any{"type": "text", "text": "{\"id\":" + itoa(nextID) + "}"}},
				"isError": false,
			}
			if os.Getenv("FIGURE_TOKEN") != "" {
				result["leaked"] = true
			}
			if req.Params.Name == "explode" {
				_ = enc.Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result":  map[string]any{"isError": true, "content": []any{}},
				})
				continue
			}
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  result,
			})
		default:
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error":   map[string]any{"code": -32601, "message": "unknown"},
			})
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func writePNG(path string) {
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	_ = png.Encode(f, img)
}
