package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

var wordSearchShortCorpus = []string{"ant", "ape", "bat", "bee", "bug", "cat", "cow", "dog", "elk", "emu", "fox", "hen", "hog", "jay", "kit", "owl", "pig", "ram", "rat", "yak", "sun", "sky", "sea", "ice", "oak", "ash", "elm", "fir", "nut", "egg"}

// Large sparse grids with many three-letter targets are adversarial to random
// filler: it often creates extra occurrences in otherwise valid placements.
// The bounded constraint fallback must succeed without weakening verification.
func TestWordSearchShortTargetsInLargeGrid(t *testing.T) {
	for _, difficulty := range []string{"easy", "medium", "hard"} {
		for seed := 0; seed < 30; seed++ {
			result := wordSearchTestResult(t, wordSearchShortCorpus, difficulty, seed, 24)
			if err := verifyWordSearchIndependently(result); err != nil {
				t.Fatalf("%s/%d: %v", difficulty, seed, err)
			}
		}
	}
}

func TestWordSearchEscapedMaximumLabelsAndGrid(t *testing.T) {
	labels := make([]string, len(wordSearchShortCorpus))
	for i, word := range wordSearchShortCorpus {
		labels[i] = word[:1] + strings.Repeat("'", 61) + word[1:]
	}
	raw, _ := json.Marshal(map[string]any{"title": strings.Repeat("'", 120), "words": labels, "difficulty": "hard", "size": 24, "seed": 42})
	value, err := Call("make_word_search", raw)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(WordSearchResult)
	if err := verifyWordSearchIndependently(result); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > 262_144 || len(result.HTML) > 98_304 || len(result.AnswerKeyHTML) > 98_304 {
		t.Fatal("escaped maximum exceeded caps", len(encoded), err)
	}
	for _, sheet := range []string{result.HTML, result.AnswerKeyHTML} {
		if strings.Count(sheet, "&#39;") < 120+61*30 {
			t.Fatal("maximum labels not escaped")
		}
	}
	t.Logf("maximum grid/labels encoded=%d, puzzleHTML=%d, keyHTML=%d bytes", len(encoded), len(result.HTML), len(result.AnswerKeyHTML))
}

func BenchmarkWordSearchShortTargets24(b *testing.B) {
	for _, difficulty := range []string{"easy", "medium", "hard"} {
		b.Run(difficulty, func(b *testing.B) {
			raw, _ := json.Marshal(map[string]any{"title": "Short words", "words": wordSearchShortCorpus, "difficulty": difficulty, "size": 24, "seed": 42})
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := Call("make_word_search", raw); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
