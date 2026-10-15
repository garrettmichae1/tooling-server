// Generate fixed synthetic HTTP fixtures for the future native iPhone caller.
// Run from the repository root; -check verifies checked-in generator behavior.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"edsger.local/figureserver/internal/studyhost"
)

type fixtureCase struct {
	name string
	args map[string]any
}

func main() {
	check := flag.Bool("check", false, "verify the checked-in fixtures without modifying files")
	flag.Parse()
	if flag.NArg() != 0 {
		fail("unexpected_argument")
	}
	words := []string{"MATRIX", "VECTOR", "SCALAR", "PIVOT", "RANK", "BASIS", "SPACE", "KERNEL", "RANGE", "IMAGE", "SPAN", "NULLITY", "ROW", "COLUMN", "LINEAR", "SYSTEM", "EQUATION", "SOLUTION", "INVERSE", "PRODUCT", "TRACE", "DETERMINANT", "EIGENVALUE", "EIGENVECTOR", "DIAGONAL", "ORTHOGONAL", "PROJECTION", "TRANSPOSE", "IDENTITY", "COEFFICIENT"}
	cases := make([]fixtureCase, 0, 16)
	for _, count := range []int{1, 3, 5, 30} {
		for _, difficulty := range []string{"easy", "medium", "hard"} {
			cases = append(cases, fixtureCase{fmt.Sprintf("%02d-%s", count, difficulty), map[string]any{"title": "Linear algebra practice", "words": words[:count], "difficulty": difficulty}})
		}
	}
	cases = append(cases,
		fixtureCase{"palindromes", map[string]any{"title": "Palindromes", "words": []string{"LEVEL", "RADAR", "CIVIC", "ROTOR"}, "difficulty": "hard", "seed": 42}},
		fixtureCase{"substrings", map[string]any{"title": "Cat words", "words": []string{"CAT", "WILDCAT", "CATFISH"}, "difficulty": "medium", "seed": 7, "size": 14}},
		fixtureCase{"normalization", map[string]any{"title": "\u0085 Nature & <letters> 🐝 \u0085", "words": []string{" Sea Lion ", "T-Rex", "Oak's"}, "difficulty": "hard", "seed": 2147483647, "size": 12}},
		fixtureCase{"zero-seed", map[string]any{"title": "Animals", "words": []string{"CAT", "DOG", "BIRD"}, "difficulty": "easy", "seed": 0, "size": 8}},
	)
	directory := filepath.Join("services", "study-host", "tests", "fixtures", "word-search")
	if !*check {
		if err := os.MkdirAll(directory, 0755); err != nil {
			fail("fixture_directory_failed")
		}
	}
	// This is deliberately a public fixed test credential, never a deployment key.
	token := strings.Repeat("fixture-", 8)
	for _, example := range cases {
		handler, err := studyhost.NewWordSearch(token)
		if err != nil {
			fail("fixture_host_failed")
		}
		input := map[string]any{"tool": "make_word_search", "arguments": example.args}
		body, err := json.Marshal(input)
		if err != nil {
			fail("fixture_input_failed")
		}
		request := httptest.NewRequest(http.MethodPost, "/v1/tools/call", bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			fmt.Fprintln(os.Stderr, "fixture_generation_failed", example.name, response.Code)
			os.Exit(1)
		}
		var parsed any
		if json.Unmarshal(response.Body.Bytes(), &parsed) != nil {
			fail("fixture_response_failed")
		}
		encoded, err := json.MarshalIndent(map[string]any{"input": input, "response": parsed}, "", "  ")
		if err != nil {
			fail("fixture_encoding_failed")
		}
		encoded = append(encoded, '\n')
		path := filepath.Join(directory, example.name+".json")
		if *check {
			expected, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(expected, encoded) {
				fmt.Fprintln(os.Stderr, "fixture_changed", example.name)
				os.Exit(1)
			}
		} else if os.WriteFile(path, encoded, 0644) != nil {
			fail("fixture_write_failed")
		}
	}
	fmt.Printf("PASS %d fixed Go HTTP word-search fixtures%s\n", len(cases), map[bool]string{true: " unchanged", false: " generated"}[*check])
}

func fail(code string) {
	fmt.Fprintln(os.Stderr, code)
	os.Exit(1)
}
