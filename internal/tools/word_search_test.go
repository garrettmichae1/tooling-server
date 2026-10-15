package tools

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var wordSearchAnimalCorpus = []string{"elephant", "giraffe", "dolphin", "penguin", "butterfly", "kangaroo", "crocodile", "rhinoceros", "hedgehog", "flamingo", "leopard", "octopus", "squirrel", "raccoon", "tortoise", "jaguar", "panther", "buffalo", "hamster", "otter", "walrus", "zebra", "falcon", "pelican", "ostrich", "gazelle", "badger", "parrot", "iguana", "cheetah"}

func wordSearchTestResult(t *testing.T, words []string, difficulty string, seed, size int) WordSearchResult {
	t.Helper()
	arguments := map[string]any{"title": "Animal discovery", "words": words, "difficulty": difficulty}
	if seed >= 0 {
		arguments["seed"] = seed
	}
	if size != 0 {
		arguments["size"] = size
	}
	raw, _ := json.Marshal(arguments)
	result, err := Call("make_word_search", raw)
	if err != nil {
		t.Fatalf("%d/%s/%d/%d: %v", len(words), difficulty, seed, size, err)
	}
	return result.(WordSearchResult)
}

// This verifier deliberately uses eight brute-force directions and physical
// coordinate sets, not the generator trie, four-axis scan or placement helpers.
func verifyWordSearchIndependently(result WordSearchResult) error {
	if len(result.Grid) != result.Size || len(result.Placements) != len(result.Words) {
		return fmt.Errorf("shape")
	}
	for _, row := range result.Grid {
		if len(row) != result.Size {
			return fmt.Errorf("width")
		}
		for _, letter := range []byte(row) {
			if letter < 'A' || letter > 'Z' {
				return fmt.Errorf("alphabet")
			}
		}
	}
	deltas := map[string][2]int{"E": {0, 1}, "W": {0, -1}, "S": {1, 0}, "N": {-1, 0}, "SE": {1, 1}, "NW": {-1, -1}, "NE": {-1, 1}, "SW": {1, -1}}
	for id, word := range result.Words {
		found := map[string]bool{}
		for row := 0; row < result.Size; row++ {
			for column := 0; column < result.Size; column++ {
				for _, delta := range deltas {
					indices := make([]int, len(word.Letters))
					matched := true
					for k, letter := range []byte(word.Letters) {
						r, c := row+k*delta[0], column+k*delta[1]
						if r < 0 || r >= result.Size || c < 0 || c >= result.Size || result.Grid[r][c] != letter {
							matched = false
							break
						}
						indices[k] = r*result.Size + c
					}
					if matched {
						sort.Ints(indices)
						found[fmt.Sprint(indices)] = true
					}
				}
			}
		}
		if len(found) != 1 {
			return fmt.Errorf("%s has %d physical occurrences", word.Letters, len(found))
		}
		p := result.Placements[id]
		if word.ID != id || p.WordID != id || len(p.Cells) != len(word.Letters) {
			return fmt.Errorf("placement shape")
		}
		allowed := false
		for _, direction := range result.Directions {
			if p.Direction == direction {
				allowed = true
			}
		}
		if !allowed {
			return fmt.Errorf("forbidden direction")
		}
		delta, known := deltas[p.Direction]
		if !known {
			return fmt.Errorf("direction")
		}
		indices := make([]int, len(p.Cells))
		for k, cell := range p.Cells {
			if cell.Row != p.Start.Row+k*delta[0] || cell.Column != p.Start.Column+k*delta[1] || cell.Row < 0 || cell.Row >= result.Size || cell.Column < 0 || cell.Column >= result.Size || result.Grid[cell.Row][cell.Column] != word.Letters[k] {
				return fmt.Errorf("placement path")
			}
			indices[k] = cell.Row*result.Size + cell.Column
		}
		if p.Start != p.Cells[0] || p.End != p.Cells[len(p.Cells)-1] {
			return fmt.Errorf("endpoints")
		}
		sort.Ints(indices)
		if !found[fmt.Sprint(indices)] {
			return fmt.Errorf("placement mismatch")
		}
	}
	return nil
}

func TestWordSearchRealisticCorpusAndReplay(t *testing.T) {
	started := time.Now()
	count := 0
	for _, wordCount := range []int{1, 3, 5, 30} {
		for _, difficulty := range []string{"easy", "medium", "hard"} {
			for _, seed := range []int{0, 1, 42, 2147483647} {
				result := wordSearchTestResult(t, wordSearchAnimalCorpus[:wordCount], difficulty, seed, 0)
				if err := verifyWordSearchIndependently(result); err != nil {
					t.Fatalf("%d/%s/%d: %v", wordCount, difficulty, seed, err)
				}
				replayed := wordSearchTestResult(t, wordSearchAnimalCorpus[:wordCount], difficulty, seed, 0)
				a, _ := json.Marshal(result)
				b, _ := json.Marshal(replayed)
				if !bytes.Equal(a, b) {
					t.Fatal("replay drift")
				}
				if len(a) > 262_144 || len(result.HTML) > 98_304 || len(result.AnswerKeyHTML) > 98_304 {
					t.Fatal("output bounds")
				}
				count++
			}
		}
	}
	t.Logf("%d independent verified puzzles, each replayed, in %s", count, time.Since(started))
}

func TestWordSearchSubstringSharingAndPalindromes(t *testing.T) {
	for _, words := range [][]string{{"cat", "wildcat", "catfish"}, {"level", "civic", "radar", "kayak"}, {"bee", "beetle", "honey bee"}, {"art", "earth", "heart"}} {
		for _, difficulty := range []string{"easy", "medium", "hard"} {
			for seed := 0; seed < 6; seed++ {
				result := wordSearchTestResult(t, words, difficulty, seed, 0)
				if err := verifyWordSearchIndependently(result); err != nil {
					t.Fatalf("%v/%s/%d: %v", words, difficulty, seed, err)
				}
			}
		}
	}
}

func TestWordSearchValidationAndFixedFailure(t *testing.T) {
	valid := map[string]any{"title": "Title", "words": []string{"garden"}, "difficulty": "easy"}
	for _, change := range []map[string]any{
		{"title": " \t\n "}, {"title": strings.Repeat("é", 61)}, {"words": []string{}}, {"words": make([]string, 31)},
		{"words": []string{"ab"}}, {"words": []string{strings.Repeat("a", 19)}}, {"words": []string{"café"}}, {"words": []string{"cat\tdog"}}, {"words": []string{"cat2"}}, {"words": []string{"cat_"}}, {"words": []string{"-cat"}}, {"words": []string{"cat-"}}, {"words": []string{" cat\n"}}, {"words": []string{"\u00a0cat"}},
		{"words": []string{"Cat", "cat"}}, {"words": []string{"ice cream", "ice-cream"}}, {"words": []string{"CAT", "TAC"}},
		{"difficulty": "extreme"}, {"size": 7}, {"size": 25}, {"size": 8.5}, {"words": []string{"butterfly"}, "size": 8}, {"seed": -1}, {"seed": 2147483648}, {"seed": 1.5}, {"seed": nil}, {"size": nil}, {"extra": true},
	} {
		arguments := map[string]any{}
		for k, v := range valid {
			arguments[k] = v
		}
		for k, v := range change {
			arguments[k] = v
		}
		raw, _ := json.Marshal(arguments)
		if _, err := Call("make_word_search", raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, raw := range []string{`{"title":"x","words":["cat"],"difficulty":"easy","seed":1,"seed":2}`, `{"title":"x","words":["cat"],"difficulty":"easy"} {}`, `[]`, `null`} {
		if _, err := Call("make_word_search", []byte(raw)); err == nil {
			t.Error("accepted malformed JSON")
		}
	}
	for _, words := range [][]string{{"AAA", "AAAA"}, {"ABA", "ABABA"}, {"CAT", "CATCAT"}} {
		raw, _ := json.Marshal(map[string]any{"title": "x", "words": words, "difficulty": "hard", "seed": 0})
		started := time.Now()
		result, err := Call("make_word_search", raw)
		if result != nil || err == nil || err.(*Error).Code != "generation_failed" || err.Error() != wordSearchFailure().Error() {
			t.Fatal("partial result or unstable failure", result, err)
		}
		if time.Since(started) > time.Second {
			t.Fatal("impossible substring did not fail promptly")
		}
	}
}

func TestWordSearchNormalizedReplayNumericAndHash(t *testing.T) {
	result, err := Call("make_word_search", []byte(`{"title":"  Garden & flowers  ","words":["  ice cream  ","honey-bee","butterfly"],"difficulty":"medium","size":16.0,"seed":1e1}`))
	if err != nil {
		t.Fatal(err)
	}
	r := result.(WordSearchResult)
	replay, err := Call("make_word_search", []byte(`{"title":"Garden & flowers","words":["ice cream","honey-bee","butterfly"],"difficulty":"medium","size":16,"seed":10}`))
	if err != nil || !reflect.DeepEqual(result, replay) {
		t.Fatal("numeric/trim canonical replay", err)
	}
	if r.Words[0].Letters != "ICECREAM" || r.Words[0].Label != "ice cream" || r.Words[1].Letters != "HONEYBEE" {
		t.Fatal("normalization")
	}
	// Independent recipe implementation uses a buffer, not generator digest.
	digest := func(parts []string) []byte {
		var data bytes.Buffer
		for _, part := range parts {
			if err := binary.Write(&data, binary.BigEndian, uint32(len([]byte(part)))); err != nil {
				t.Fatal(err)
			}
			data.WriteString(part)
		}
		sum := sha256.Sum256(data.Bytes())
		return sum[:]
	}
	parts := []string{"wordsearch-v1", r.Title, r.Difficulty, strconv.Itoa(r.Size), strconv.Itoa(r.Seed), strconv.Itoa(len(r.Words))}
	for _, word := range r.Words {
		parts = append(parts, word.Label, word.Letters)
	}
	parts = append(parts, r.Grid...)
	for _, p := range r.Placements {
		parts = append(parts, p.Direction, strconv.Itoa(p.Start.Row), strconv.Itoa(p.Start.Column), strconv.Itoa(p.End.Row), strconv.Itoa(p.End.Column))
	}
	if r.PuzzleID != hex.EncodeToString(digest(parts)) {
		t.Fatal("puzzle hash recipe")
	}
	raw := `{"title":"  Garden & flowers ","words":[" ice cream ","honey-bee","butterfly"],"difficulty":"medium","size":16}`
	generated, err := Call("make_word_search", []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	noSeed := generated.(WordSearchResult)
	seedParts := []string{"wordsearch-v1-seed", r.Title, r.Difficulty, "16", "3"}
	for _, word := range r.Words {
		seedParts = append(seedParts, word.Label, word.Letters)
	}
	if noSeed.Seed != int(binary.BigEndian.Uint32(digest(seedParts)[:4])&0x7fffffff) {
		t.Fatal("seed hash recipe")
	}
	seedRaw, _ := json.Marshal(map[string]any{"title": r.Title, "words": []string{"ice cream", "honey-bee", "butterfly"}, "difficulty": "medium", "size": 16, "seed": noSeed.Seed})
	explicit, err := Call("make_word_search", seedRaw)
	if err != nil || !reflect.DeepEqual(generated, explicit) {
		t.Fatal("missing seed replay", err)
	}
}

func TestWordSearchHTMLHasSeparatedEscapedExports(t *testing.T) {
	raw := `{"title":"<script>alert(1)</script> & \"test\"","words":["jack's","ice-cream"],"difficulty":"hard","seed":42}`
	value, err := Call("make_word_search", []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	r := value.(WordSearchResult)
	for _, sheet := range []string{r.HTML, r.AnswerKeyHTML} {
		if strings.Contains(sheet, "<script>") || strings.Contains(sheet, "<iframe") || strings.Contains(sheet, "http:") || strings.Contains(sheet, "https:") || !strings.Contains(sheet, "&lt;script&gt;") || !strings.Contains(sheet, "jack&#39;s") || !strings.Contains(sheet, "default-src 'none'") {
			t.Fatal("untrusted markup/remote assets or missing escaping")
		}
	}
	if strings.Contains(r.HTML, `class="found"`) || strings.Contains(r.HTML, `class="coordinates"`) || strings.Contains(r.HTML, `class="solution"`) {
		t.Fatal("solutions in puzzle sheet")
	}
	if !strings.Contains(r.AnswerKeyHTML, `class="found"`) || !strings.Contains(r.AnswerKeyHTML, `class="coordinates"`) {
		t.Fatal("key has no solutions")
	}
	if !r.Offline || r.NetworkRequests || r.PreviewRequiresScripts || !r.PuzzleVerified || r.ContentVerified {
		t.Fatal("incorrect capability flags")
	}
	var object map[string]json.RawMessage
	encoded, _ := json.Marshal(r)
	if json.Unmarshal(encoded, &object) != nil || len(object) != 20 {
		t.Fatalf("contract keys %d", len(object))
	}
}

func TestWordSearchParallelReplayAndBoundedAdversarialCorpus(t *testing.T) {
	var wg sync.WaitGroup
	for seed := 0; seed < 8; seed++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			r := wordSearchTestResult(t, wordSearchAnimalCorpus, "hard", seed, 24)
			if err := verifyWordSearchIndependently(r); err != nil {
				t.Error(err)
			}
		}(seed)
	}
	wg.Wait()
	for _, words := range [][]string{{"CAT", "ATC"}, {"AAA", "AAB", "ABA", "ABB", "BAB", "BBB"}, {"CAT", "SCATTER", "CATCH", "BOBCAT", "CATFISH"}, {"ABCDE", "BCDEF", "CDEFG", "DEFGH", "EFGHI", "FGHIJ", "GHIJK", "HIJKL"}} {
		for _, difficulty := range []string{"easy", "medium", "hard"} {
			raw, _ := json.Marshal(map[string]any{"title": "Adversarial", "words": words, "difficulty": difficulty, "size": 8, "seed": 0})
			value, err := Call("make_word_search", raw)
			if err != nil {
				if err.(*Error).Code != "generation_failed" {
					t.Fatal(err)
				}
				continue
			}
			if err := verifyWordSearchIndependently(value.(WordSearchResult)); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestWordSearchGeneratedPropertyCorpus(t *testing.T) {
	random := rand.New(rand.NewSource(238971))
	verified, failed := 0, 0
	for sample := 0; sample < 240; sample++ {
		count := []int{1, 3, 5, 30}[sample%4]
		words := make([]string, 0, count)
		seen := map[string]bool{}
		for len(words) < count {
			letters := make([]byte, 3+random.Intn(16))
			for index := range letters {
				letters[index] = 'A' + byte(random.Intn(26))
			}
			word, reversed := string(letters), make([]byte, len(letters))
			for index := range letters {
				reversed[index] = letters[len(letters)-1-index]
			}
			if !seen[word] && !seen[string(reversed)] {
				words = append(words, word)
				seen[word] = true
			}
		}
		arguments := map[string]any{"title": "Property corpus", "words": words, "difficulty": []string{"easy", "medium", "hard"}[sample%3], "seed": random.Int31()}
		if sample%5 == 0 {
			arguments["size"] = 24
		}
		raw, _ := json.Marshal(arguments)
		result, err := Call("make_word_search", raw)
		if err != nil {
			if result != nil || err.(*Error).Code != "generation_failed" {
				t.Fatal("partial artifact or unexpected error", err)
			}
			failed++
			continue
		}
		if err := verifyWordSearchIndependently(result.(WordSearchResult)); err != nil {
			t.Fatalf("property sample %d: %v", sample, err)
		}
		verified++
	}
	if verified == 0 {
		t.Fatal("property corpus never exercised successful generation")
	}
	t.Logf("independently verified %d arbitrary-word puzzles, %d fixed failures", verified, failed)
}

func TestWordSearchWorkCeilingAndDenseRequests(t *testing.T) {
	words := []WordSearchWord{{ID: 0, Label: "garden", Letters: "GARDEN"}}
	search := newWordSearchSearch(words, wordSearchAllDirections)
	search.work = wordSearchWorkLimit - 1
	if hits, ok := search.scan(make([]byte, 24*24), 24); ok || hits != nil || search.work != wordSearchWorkLimit {
		t.Fatal("work ceiling failed closed")
	}
	// Thirty distinct equal-length words in a tiny alphabet stress collisions,
	// tightly packed grids, and full-input failure without silent omissions.
	dense := []string{}
	seen := map[string]bool{}
	for value := 0; len(dense) < 30; value++ {
		letters := make([]byte, 8)
		n := value
		for index := range letters {
			letters[index] = "ABC"[n%3]
			n /= 3
		}
		word, reverse := string(letters), make([]byte, len(letters))
		for index := range letters {
			reverse[index] = letters[len(letters)-1-index]
		}
		if !seen[word] && !seen[string(reverse)] {
			seen[word] = true
			dense = append(dense, word)
		}
	}
	for _, difficulty := range []string{"easy", "medium", "hard"} {
		for _, size := range []int{8, 24} {
			raw, _ := json.Marshal(map[string]any{"title": "Dense stress", "words": dense, "difficulty": difficulty, "size": size, "seed": 0})
			started := time.Now()
			result, err := Call("make_word_search", raw)
			if err != nil {
				if result != nil || err.(*Error).Code != "generation_failed" {
					t.Fatal("unexpected dense result", err)
				}
			} else if err := verifyWordSearchIndependently(result.(WordSearchResult)); err != nil {
				t.Fatal(err)
			}
			t.Logf("dense %s/%d completed in %s", difficulty, size, time.Since(started))
		}
	}
}

func BenchmarkWordSearchRealistic(b *testing.B) {
	for _, count := range []int{1, 3, 5, 30} {
		for _, difficulty := range []string{"easy", "medium", "hard"} {
			b.Run(fmt.Sprintf("%d/%s", count, difficulty), func(b *testing.B) {
				raw, _ := json.Marshal(map[string]any{"title": "Animals", "words": wordSearchAnimalCorpus[:count], "difficulty": difficulty, "seed": 42})
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if _, err := Call("make_word_search", raw); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
