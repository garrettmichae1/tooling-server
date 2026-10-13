package studyhost

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

var wordSearchTestWords = []string{"CATS", "DOGS", "EMUS", "OWLS", "FOXES", "ANTS", "BEES", "COWS", "ELKS", "HENS", "YAKS", "RAMS", "EELS", "APES", "BISON", "PIGS", "RATS", "LIONS", "BEARS", "WOLVES", "DEER", "SEALS", "FROGS", "TOADS", "HARES", "MULES", "GOATS", "IBIS", "MOLES", "CRABS"}

func wordSearchFixture(count int, difficulty string) string {
	words := wordSearchTestWords
	if count < len(words) {
		words = words[:count]
	}
	raw, _ := json.Marshal(map[string]any{"tool": "make_word_search", "arguments": map[string]any{"title": "Animal word search", "words": words, "difficulty": difficulty, "size": 24, "seed": 7}})
	return string(raw)
}

func TestWordSearchOriginIsolationAndDeterministicFixtures(t *testing.T) {
	for _, count := range []int{1, 3, 5, 30} {
		for _, difficulty := range []string{"easy", "medium", "hard"} {
			h, err := NewWordSearch(testToken)
			if err != nil {
				t.Fatal(err)
			}
			body := wordSearchFixture(count, difficulty)
			first := request(h, "POST", "/v1/tools/call", body, testToken)
			if first.Code != http.StatusOK {
				t.Fatal("fixed word-search fixture rejected", count, difficulty, first.Code)
			}
			second := request(h, "POST", "/v1/tools/call", body, testToken)
			if !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
				t.Fatal("word-search replay changed")
			}
			if len(first.Body.Bytes()) > 262_144 {
				t.Fatal("word-search origin result exceeded budget")
			}
			for _, foreign := range []string{fixture(1), worksheetFixture(1), `{"tool":"calculate","arguments":{"expression":"1+1"}}`} {
				if request(h, "POST", "/v1/tools/call", foreign, testToken).Code != http.StatusBadRequest {
					t.Fatal("word-search process admitted a foreign tool")
				}
			}
			worksheet, _ := NewWorksheet(testToken)
			for _, foreign := range []http.Handler{handler(t), worksheet} {
				if request(foreign, "POST", "/v1/tools/call", body, testToken).Code != http.StatusBadRequest {
					t.Fatal("existing process admitted word search")
				}
			}
			if request(h, "POST", "/v1/tools/call", body, "").Code != http.StatusUnauthorized {
				t.Fatal("word-search origin accepted an anonymous caller")
			}
			if request(h, "GET", "/v1/tools", "", testToken).Code != http.StatusNotFound {
				t.Fatal("private origin exposed a catalog")
			}
		}
	}
}

func TestWordSearchOriginBoundsAndFixedFailures(t *testing.T) {
	h, _ := NewWordSearch(testToken)
	if request(h, "POST", "/v1/tools/call", strings.Repeat(" ", 16_385), testToken).Code != http.StatusRequestEntityTooLarge {
		t.Fatal("word-search origin did not enforce its own smaller request cap")
	}
	for _, body := range []string{wordSearchFixture(0, "easy"), `{"tool":"make_word_search","arguments":{"title":"Secret supplied content","words":["éclair"],"difficulty":"easy"}}`, `{"tool":"make_word_search","arguments":{"title":"Animals","words":["CAT","TAC"],"difficulty":"easy"}}`} {
		response := request(h, "POST", "/v1/tools/call", body, testToken)
		if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "Secret supplied content") {
			t.Fatal("word-search input failure was not fixed and bounded")
		}
	}
	response := request(h, "POST", "/v1/tools/call", `{"tool":"make_word_search","arguments":{"title":"Fixed puzzle","words":["CAT","CATCAT"],"difficulty":"hard"}}`, testToken)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), `"code":"generation_failed"`) || strings.Contains(response.Body.String(), "CAT") {
		t.Fatal("unsatisfiable words did not return a sanitized generation failure")
	}
}
