package tools

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

// parseJSON is shared by the envelope, schema validation, and JSON utility.
// It preserves numeric precision and rejects duplicate keys before decoding.
func parseJSON(raw []byte) (any, error) {
	if !utf8.Valid(raw) {
		return nil, invalid("arguments", "JSON must be valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	value, err := readJSONValue(dec, 0)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, invalid("arguments", "send only one JSON value")
	}
	return value, nil
}

// NormalizeJSON validates depth, duplicate keys, UTF-8, and trailing content.
func NormalizeJSON(raw []byte) (json.RawMessage, error) {
	value, err := parseJSON(raw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func readJSONValue(dec *json.Decoder, depth int) (any, error) {
	if depth > 32 {
		return nil, invalid("arguments", "JSON nesting exceeds 32 levels")
	}
	token, err := dec.Token()
	if err != nil {
		return nil, invalid("arguments", "invalid JSON syntax")
	}
	if delimiter, ok := token.(json.Delim); ok {
		switch delimiter {
		case '{':
			obj := map[string]any{}
			for dec.More() {
				keyToken, err := dec.Token()
				key, ok := keyToken.(string)
				if err != nil || !ok {
					return nil, invalid("arguments", "invalid JSON object key")
				}
				if _, ok := obj[key]; ok {
					return nil, invalid("arguments", "duplicate JSON object keys are not allowed")
				}
				value, err := readJSONValue(dec, depth+1)
				if err != nil {
					return nil, err
				}
				obj[key] = value
			}
			closing, err := dec.Token()
			if err != nil || closing != json.Delim('}') {
				return nil, invalid("arguments", "invalid JSON object")
			}
			return obj, nil
		case '[':
			arr := make([]any, 0)
			for dec.More() {
				value, err := readJSONValue(dec, depth+1)
				if err != nil {
					return nil, err
				}
				arr = append(arr, value)
			}
			closing, err := dec.Token()
			if err != nil || closing != json.Delim(']') {
				return nil, invalid("arguments", "invalid JSON array")
			}
			return arr, nil
		default:
			return nil, invalid("arguments", "unexpected JSON delimiter")
		}
	}
	return token, nil
}

func inspectJSON(raw json.RawMessage) (any, error) {
	var in struct {
		JSON string `json:"json"`
	}
	decode(raw, &in)
	value, err := parseJSON([]byte(in.JSON))
	if err != nil {
		return nil, invalid("arguments.json", err.Error())
	}
	formatted, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, invalid("arguments.json", "could not format JSON")
	}
	kind := "null"
	switch value.(type) {
	case map[string]any:
		kind = "object"
	case []any:
		kind = "array"
	case string:
		kind = "string"
	case json.Number:
		kind = "number"
	case bool:
		kind = "boolean"
	}
	return map[string]any{"valid": true, "root_type": kind, "formatted": string(formatted)}, nil
}

func testRegex(raw json.RawMessage) (any, error) {
	var in struct{ Pattern, Text string }
	decode(raw, &in)
	re, err := regexp.Compile(in.Pattern)
	if err != nil {
		return nil, invalid("arguments.pattern", "invalid RE2 pattern; lookaround and backreferences are unsupported")
	}
	indices := re.FindAllStringSubmatchIndex(in.Text, 51)
	truncated := len(indices) > 50
	if truncated {
		indices = indices[:50]
	}
	matches := make([]map[string]any, 0, len(indices))
	outputBytes := 0
	for _, index := range indices {
		bytes := index[1] - index[0]
		for i := 2; i < len(index); i += 2 {
			if index[i] >= 0 {
				bytes += index[i+1] - index[i]
			}
		}
		if outputBytes+bytes > 128*1024 {
			truncated = true
			break
		}
		outputBytes += bytes
		groups := make([]any, 0, len(index)/2-1)
		for i := 2; i < len(index); i += 2 {
			if index[i] < 0 {
				groups = append(groups, nil)
			} else {
				groups = append(groups, in.Text[index[i]:index[i+1]])
			}
		}
		matches = append(matches, map[string]any{"text": in.Text[index[0]:index[1]], "start_byte": index[0], "end_byte": index[1], "groups": groups})
	}
	return map[string]any{"matches": matches, "count": len(matches), "truncated": truncated, "offset_unit": "UTF-8 bytes", "engine": "Go RE2"}, nil
}

func encodeText(raw json.RawMessage) (any, error) {
	var in struct{ Operation, Format, Text string }
	decode(raw, &in)
	var result string
	var err error
	if in.Operation == "encode" {
		switch in.Format {
		case "base64":
			result = base64.StdEncoding.EncodeToString([]byte(in.Text))
		case "hex":
			result = hex.EncodeToString([]byte(in.Text))
		case "url_query":
			result = url.QueryEscape(in.Text)
		}
	} else {
		var decoded []byte
		switch in.Format {
		case "base64":
			decoded, err = base64.StdEncoding.Strict().DecodeString(in.Text)
			result = string(decoded)
		case "hex":
			decoded, err = hex.DecodeString(in.Text)
			result = string(decoded)
		case "url_query":
			result, err = url.QueryUnescape(in.Text)
		}
	}
	if err != nil {
		return nil, invalid("arguments.text", "text is invalid for the selected encoding")
	}
	if !utf8.ValidString(result) || strings.ContainsRune(result, 0) {
		return nil, invalid("arguments.text", "decoded bytes must be UTF-8 text without nulls")
	}
	if in.Operation == "decode" && len(result) > 64*1024 {
		return nil, invalid("arguments.text", "decoded text exceeds 64 KiB")
	}
	return map[string]any{"text": result, "format": in.Format, "operation": in.Operation}, nil
}

func hashText(raw json.RawMessage) (any, error) {
	var in struct{ Algorithm, Text string }
	decode(raw, &in)
	var hash string
	if in.Algorithm == "sha256" {
		sum := sha256.Sum256([]byte(in.Text))
		hash = hex.EncodeToString(sum[:])
	} else {
		sum := sha512.Sum512([]byte(in.Text))
		hash = hex.EncodeToString(sum[:])
	}
	return map[string]any{"algorithm": in.Algorithm, "hex": hash, "input_bytes": len(in.Text)}, nil
}
