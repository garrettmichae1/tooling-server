package tools

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
)

const WordSearchGeneratorVersion = "wordsearch-v1"

// These ceilings bound work, not wall-clock time, and are part of v1 behavior.
const (
	wordSearchWorkLimit      = 32_000_000
	wordSearchRestarts       = 4
	wordSearchCandidateLimit = 96
	wordSearchFillRestarts   = 8
	wordSearchFillerLetters  = 26
)

type WordSearchWord struct {
	ID      int    `json:"id"`
	Label   string `json:"label"`
	Letters string `json:"letters"`
}
type WordSearchCell struct {
	Row    int `json:"row"`
	Column int `json:"column"`
}
type WordSearchPlacement struct {
	WordID    int              `json:"word_id"`
	Direction string           `json:"direction"`
	Start     WordSearchCell   `json:"start"`
	End       WordSearchCell   `json:"end"`
	Cells     []WordSearchCell `json:"cells"`
}
type WordSearchResult struct {
	SchemaVersion          int                   `json:"schema_version"`
	GeneratorVersion       string                `json:"generator_version"`
	PuzzleID               string                `json:"puzzle_id"`
	Title                  string                `json:"title"`
	Difficulty             string                `json:"difficulty"`
	Size                   int                   `json:"size"`
	Seed                   int                   `json:"seed"`
	Directions             []string              `json:"directions"`
	Grid                   []string              `json:"grid"`
	Words                  []WordSearchWord      `json:"words"`
	Placements             []WordSearchPlacement `json:"placements"`
	Filename               string                `json:"filename"`
	MediaType              string                `json:"media_type"`
	HTML                   string                `json:"html"`
	AnswerKeyHTML          string                `json:"answer_key_html"`
	Offline                bool                  `json:"offline"`
	NetworkRequests        bool                  `json:"network_requests"`
	PreviewRequiresScripts bool                  `json:"preview_requires_scripts"`
	PuzzleVerified         bool                  `json:"puzzle_verified"`
	ContentVerified        bool                  `json:"content_verified"`
}

type wordSearchInput struct {
	Title      string       `json:"title"`
	Words      []string     `json:"words"`
	Difficulty string       `json:"difficulty"`
	Size       *json.Number `json:"size"`
	Seed       *json.Number `json:"seed"`
}

type wordSearchDirection struct {
	name        string
	row, column int
}

var wordSearchAllDirections = []wordSearchDirection{{"E", 0, 1}, {"S", 1, 0}, {"SE", 1, 1}, {"NE", -1, 1}, {"W", 0, -1}, {"N", -1, 0}, {"NW", -1, -1}, {"SW", 1, -1}}

func wordSearchFailure() *Error {
	return &Error{Code: "generation_failed", Message: "could not construct a uniquely solvable puzzle within generation limits; try a larger size, another seed, or revise the word list"}
}

func makeWordSearch(raw json.RawMessage) (any, error) {
	var in wordSearchInput
	if len(raw) > 16_384 || json.Unmarshal(raw, &in) != nil {
		return nil, invalid("arguments", "invalid or oversized word-search arguments")
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return nil, invalid("arguments.title", "title must be nonblank")
	}
	words := make([]WordSearchWord, len(in.Words))
	seen := map[string]bool{}
	longest, total := 0, 0
	for i, label := range in.Words {
		label = strings.Trim(label, " ")
		letters, err := wordSearchLetters(label)
		if err != nil {
			return nil, invalid("arguments.words["+strconv.Itoa(i)+"]", err.Error())
		}
		if seen[letters] || seen[wordSearchReverse(letters)] {
			return nil, invalid("arguments.words", "duplicate or reversed target words are not allowed")
		}
		seen[letters] = true
		words[i] = WordSearchWord{i, label, letters}
		longest = max(longest, len(letters))
		total += len(letters)
	}
	size := 0
	if in.Size != nil {
		value, _ := in.Size.Float64()
		size = int(value)
	}
	if size != 0 && size < longest {
		return nil, invalid("arguments.size", "size is smaller than a target word")
	}
	seed := 0
	if in.Seed != nil {
		value, _ := in.Seed.Float64()
		seed = int(value)
	} else {
		parts := []string{"wordsearch-v1-seed", in.Title, in.Difficulty, strconv.Itoa(size), strconv.Itoa(len(words))}
		for _, word := range words {
			parts = append(parts, word.Label, word.Letters)
		}
		digest := wordSearchDigest(parts)
		seed = int(binary.BigEndian.Uint32(digest[:4]) & 0x7fffffff)
	}
	directionCount := 2
	if in.Difficulty == "medium" {
		directionCount = 4
	} else if in.Difficulty == "hard" {
		directionCount = 8
	}
	directions := wordSearchAllDirections[:directionCount]
	// More than one embedded physical occurrence can never be repaired by filler.
	for _, long := range words {
		for _, short := range words {
			if long.ID == short.ID || len(short.Letters) > len(long.Letters) {
				continue
			}
			count := 0
			reversed := wordSearchReverse(short.Letters)
			for offset := 0; offset+len(short.Letters) <= len(long.Letters); offset++ {
				part := long.Letters[offset : offset+len(short.Letters)]
				if part == short.Letters || part == reversed {
					count++
				}
			}
			if count > 1 {
				return nil, wordSearchFailure()
			}
		}
	}
	initialSize := size
	if initialSize == 0 {
		initialSize = min(24, max(8, max(longest, int(math.Ceil(math.Sqrt(float64(total)*1.7))))))
	}
	lastSize := size
	if lastSize == 0 {
		lastSize = 24
	}
	search := newWordSearchSearch(words, directions)
	for currentSize := initialSize; currentSize <= lastSize; currentSize++ {
		for restart := 0; restart < wordSearchRestarts; restart++ {
			grid, placements, ok := search.generate(currentSize, uint64(seed), restart)
			if !ok {
				if search.work >= wordSearchWorkLimit {
					return nil, wordSearchFailure()
				}
				continue
			}
			result := WordSearchResult{SchemaVersion: 1, GeneratorVersion: WordSearchGeneratorVersion, Title: in.Title, Difficulty: in.Difficulty, Size: currentSize, Seed: seed, Grid: grid, Words: words, Placements: placements, Filename: "word-search.html", MediaType: "text/html; charset=utf-8", Offline: true, PuzzleVerified: true}
			for _, direction := range directions {
				result.Directions = append(result.Directions, direction.name)
			}
			result.PuzzleID = wordSearchPuzzleID(result)
			result.HTML = wordSearchHTML(result, false)
			result.AnswerKeyHTML = wordSearchHTML(result, true)
			encoded, err := json.Marshal(result)
			if err != nil || len(result.HTML) > 98_304 || len(result.AnswerKeyHTML) > 98_304 || len(encoded) > 262_144 {
				return nil, &Error{Code: "result_too_large", Message: "word-search exceeds output limits"}
			}
			return result, nil
		}
	}
	return nil, wordSearchFailure()
}

func wordSearchLetters(label string) (string, error) {
	if len(label) == 0 || !wordSearchASCIIAlpha(label[0]) || !wordSearchASCIIAlpha(label[len(label)-1]) {
		return "", invalid("words", "words must begin and end with ASCII letters")
	}
	var b strings.Builder
	for _, letter := range []byte(label) {
		if letter == ' ' || letter == '-' || letter == '\'' {
			continue
		}
		if !wordSearchASCIIAlpha(letter) {
			return "", invalid("words", "words accept only ASCII letters and interior spaces, hyphens, apostrophes")
		}
		if letter >= 'a' && letter <= 'z' {
			letter -= 'a' - 'A'
		}
		b.WriteByte(letter)
	}
	if b.Len() < 3 || b.Len() > 18 {
		return "", invalid("words", "words must contain 3 to 18 letters")
	}
	return b.String(), nil
}
func wordSearchASCIIAlpha(b byte) bool { return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' }
func wordSearchReverse(s string) string {
	b := []byte(s)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}
func wordSearchDigest(parts []string) [32]byte {
	hash := sha256.New()
	var length [4]byte
	for _, part := range parts {
		binary.BigEndian.PutUint32(length[:], uint32(len(part)))
		hash.Write(length[:])
		hash.Write([]byte(part))
	}
	var digest [32]byte
	copy(digest[:], hash.Sum(nil))
	return digest
}
func wordSearchPuzzleID(r WordSearchResult) string {
	parts := []string{r.GeneratorVersion, r.Title, r.Difficulty, strconv.Itoa(r.Size), strconv.Itoa(r.Seed), strconv.Itoa(len(r.Words))}
	for _, word := range r.Words {
		parts = append(parts, word.Label, word.Letters)
	}
	parts = append(parts, r.Grid...)
	for _, p := range r.Placements {
		parts = append(parts, p.Direction, strconv.Itoa(p.Start.Row), strconv.Itoa(p.Start.Column), strconv.Itoa(p.End.Row), strconv.Itoa(p.End.Column))
	}
	digest := wordSearchDigest(parts)
	return hex.EncodeToString(digest[:])
}

// A fixed SplitMix64 stream avoids dependence on math/rand versions/global state.
type wordSearchRandom uint64

func (r *wordSearchRandom) next() uint64 {
	*r += 0x9e3779b97f4a7c15
	z := uint64(*r)
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

type wordSearchNode struct {
	next     [26]*wordSearchNode
	id       int
	reversed bool
}
type wordSearchHit struct {
	present   bool
	direction wordSearchDirection
	start     WordSearchCell
}
type wordSearchSearch struct {
	words      []WordSearchWord
	directions []wordSearchDirection
	trie       *wordSearchNode
	work       int
}

func newWordSearchSearch(words []WordSearchWord, directions []wordSearchDirection) *wordSearchSearch {
	s := &wordSearchSearch{words: words, directions: directions, trie: &wordSearchNode{id: -1}}
	for _, word := range words {
		for _, reversed := range []bool{false, true} {
			letters := word.Letters
			if reversed {
				letters = wordSearchReverse(letters)
				if letters == word.Letters {
					continue
				}
			}
			node := s.trie
			for _, letter := range []byte(letters) {
				index := letter - 'A'
				if node.next[index] == nil {
					node.next[index] = &wordSearchNode{id: -1}
				}
				node = node.next[index]
			}
			node.id = word.ID
			node.reversed = reversed
		}
	}
	return s
}

// Scan four canonical axes and both target spellings. A palindrome is inserted
// once, so its reversed traversal on identical physical cells is deduplicated.
// Every match must also be expressible in the requested difficulty directions.
func (s *wordSearchSearch) scan(grid []byte, size int) ([]wordSearchHit, bool) {
	hits := make([]wordSearchHit, len(s.words))
	for row := 0; row < size; row++ {
		for column := 0; column < size; column++ {
			for _, axis := range wordSearchAllDirections[:4] {
				s.work++
				if s.work >= wordSearchWorkLimit {
					return nil, false
				}
				node := s.trie
				for r, c, depth := row, column, 0; r >= 0 && r < size && c >= 0 && c < size && depth < 18; r, c, depth = r+axis.row, c+axis.column, depth+1 {
					s.work++
					if s.work >= wordSearchWorkLimit {
						return nil, false
					}
					letter := grid[r*size+c]
					if letter == 0 {
						break
					}
					node = node.next[letter-'A']
					if node == nil {
						break
					}
					if node.id >= 0 {
						if hits[node.id].present {
							return nil, false
						}
						direction, start := axis, WordSearchCell{row, column}
						if node.reversed {
							direction = wordSearchDirection{wordSearchAllDirections[4+wordSearchDirectionIndex(axis.name)].name, -axis.row, -axis.column}
							start = WordSearchCell{r, c}
						}
						allowed := false
						for _, d := range s.directions {
							if direction.name == d.name {
								allowed = true
							}
						}
						if !allowed {
							return nil, false
						}
						hits[node.id] = wordSearchHit{true, direction, start}
					}
				}
			}
		}
	}
	return hits, true
}
func wordSearchDirectionIndex(name string) int {
	for i, d := range wordSearchAllDirections {
		if d.name == name {
			return i
		}
	}
	return -1
}
func wordSearchPlacement(word WordSearchWord, hit wordSearchHit) WordSearchPlacement {
	p := WordSearchPlacement{WordID: word.ID, Direction: hit.direction.name, Start: hit.start, Cells: make([]WordSearchCell, len(word.Letters))}
	for i := range p.Cells {
		p.Cells[i] = WordSearchCell{hit.start.Row + i*hit.direction.row, hit.start.Column + i*hit.direction.column}
	}
	p.End = p.Cells[len(p.Cells)-1]
	return p
}

type wordSearchCandidate struct {
	hit     wordSearchHit
	overlap int
}

func (s *wordSearchSearch) generate(size int, seed uint64, restart int) ([]string, []WordSearchPlacement, bool) {
	random := wordSearchRandom(seed ^ uint64(size)*0xd6e8feb86659fd93 ^ uint64(restart)*0xa0761d6478bd642f)
	grid := make([]byte, size*size)
	placements := make([]WordSearchPlacement, len(s.words))
	order := make([]int, len(s.words))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return len(s.words[order[i]].Letters) > len(s.words[order[j]].Letters) })
	for _, id := range order {
		hits, ok := s.scan(grid, size)
		if !ok {
			return nil, nil, false
		}
		word := s.words[id]
		if hits[id].present {
			placements[id] = wordSearchPlacement(word, hits[id])
			continue
		}
		candidates := make([]wordSearchCandidate, 0, size*size*len(s.directions))
		for _, direction := range s.directions {
			for row := 0; row < size; row++ {
				for column := 0; column < size; column++ {
					endRow, endColumn := row+(len(word.Letters)-1)*direction.row, column+(len(word.Letters)-1)*direction.column
					if endRow < 0 || endRow >= size || endColumn < 0 || endColumn >= size {
						continue
					}
					overlap, compatible := 0, true
					for i, letter := range []byte(word.Letters) {
						cell := grid[(row+i*direction.row)*size+column+i*direction.column]
						if cell != 0 && cell != letter {
							compatible = false
							break
						}
						if cell == letter {
							overlap++
						}
					}
					if compatible {
						candidates = append(candidates, wordSearchCandidate{wordSearchHit{true, direction, WordSearchCell{row, column}}, overlap})
					}
				}
			}
		}
		for i := len(candidates) - 1; i > 0; i-- {
			j := int(random.next() % uint64(i+1))
			candidates[i], candidates[j] = candidates[j], candidates[i]
		}
		sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].overlap > candidates[j].overlap })
		placed := false
		for attempt, candidate := range candidates {
			if attempt >= wordSearchCandidateLimit {
				break
			}
			changed := make([]int, 0, len(word.Letters))
			for i, letter := range []byte(word.Letters) {
				index := (candidate.hit.start.Row+i*candidate.hit.direction.row)*size + candidate.hit.start.Column + i*candidate.hit.direction.column
				if grid[index] == 0 {
					grid[index] = letter
					changed = append(changed, index)
				}
			}
			if checkedHits, ok := s.scan(grid, size); ok {
				placements[id] = wordSearchPlacement(word, checkedHits[id])
				placed = true
				break
			}
			for _, index := range changed {
				grid[index] = 0
			}
			if s.work >= wordSearchWorkLimit {
				return nil, nil, false
			}
		}
		if !placed {
			return nil, nil, false
		}
	}
	blank := make([]int, 0, size*size)
	for index, letter := range grid {
		if letter == 0 {
			blank = append(blank, index)
		}
	}
	for fill := 0; fill < wordSearchFillRestarts; fill++ {
		for _, index := range blank {
			grid[index] = 'A' + byte(random.next()%26)
		}
		hits, ok := s.scan(grid, size)
		if !ok {
			if s.work >= wordSearchWorkLimit {
				return nil, nil, false
			}
			continue
		}
		for id, hit := range hits {
			if !hit.present {
				return nil, nil, false
			}
			expected := wordSearchPlacement(s.words[id], hit)
			if expected.Start != placements[id].Start || expected.End != placements[id].End {
				return nil, nil, false
			}
		}
		rows := make([]string, size)
		for row := range rows {
			rows[row] = string(grid[row*size : (row+1)*size])
		}
		return rows, placements, true
	}
	// Large grids with many short targets almost always gain accidental words
	// from independent random filler. Keep successful random-fill behavior, then
	// complete each blank under the same bounded uniqueness constraint instead.
	for _, index := range blank {
		grid[index] = 0
	}
	for _, index := range blank {
		alphabet := make([]byte, wordSearchFillerLetters)
		for i := range alphabet {
			alphabet[i] = 'A' + byte(i)
		}
		for i := len(alphabet) - 1; i > 0; i-- {
			j := int(random.next() % uint64(i+1))
			alphabet[i], alphabet[j] = alphabet[j], alphabet[i]
		}
		filled := false
		for _, letter := range alphabet {
			grid[index] = letter
			if s.fillerSafe(grid, size, index) {
				filled = true
				break
			}
			if s.work >= wordSearchWorkLimit {
				return nil, nil, false
			}
		}
		if !filled {
			return nil, nil, false
		}
	}
	hits, ok := s.scan(grid, size)
	if !ok {
		return nil, nil, false
	}
	for id, hit := range hits {
		if !hit.present {
			return nil, nil, false
		}
		expected := wordSearchPlacement(s.words[id], hit)
		if expected.Start != placements[id].Start || expected.End != placements[id].End {
			return nil, nil, false
		}
	}
	rows := make([]string, size)
	for row := range rows {
		rows[row] = string(grid[row*size : (row+1)*size])
	}
	return rows, placements, true
}

// All desired occurrences already exist entirely in placed letters. Therefore
// any completed target crossing this formerly blank cell would be an unwanted
// second physical occurrence, regardless of orientation. Scan only windows
// containing this cell, with both spellings in the existing trie.
func (s *wordSearchSearch) fillerSafe(grid []byte, size, index int) bool {
	row, column := index/size, index%size
	for _, axis := range wordSearchAllDirections[:4] {
		for offset := 0; offset < 18; offset++ {
			r, c := row-offset*axis.row, column-offset*axis.column
			if r < 0 || r >= size || c < 0 || c >= size {
				continue
			}
			s.work++
			if s.work >= wordSearchWorkLimit {
				return false
			}
			node := s.trie
			for depth := 0; r >= 0 && r < size && c >= 0 && c < size && depth < 18; r, c, depth = r+axis.row, c+axis.column, depth+1 {
				s.work++
				if s.work >= wordSearchWorkLimit {
					return false
				}
				letter := grid[r*size+c]
				if letter == 0 {
					break
				}
				node = node.next[letter-'A']
				if node == nil {
					break
				}
				if depth >= offset && node.id >= 0 {
					return false
				}
			}
		}
	}
	return true
}
