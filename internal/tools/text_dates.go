package tools

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func dateMath(raw json.RawMessage) (any, error) {
	var in struct {
		Start string   `json:"start"`
		End   *string  `json:"end"`
		Days  *float64 `json:"days"`
	}
	decode(raw, &in)
	if (in.End == nil) == (in.Days == nil) {
		return nil, invalid("arguments", "supply exactly one of days or end")
	}
	start, err := time.Parse(time.DateOnly, in.Start)
	if err != nil {
		return nil, invalid("arguments.start", "use a valid date in YYYY-MM-DD format")
	}
	var end time.Time
	var days int
	if in.End != nil {
		end, err = time.Parse(time.DateOnly, *in.End)
		if err != nil {
			return nil, invalid("arguments.end", "use a valid date in YYYY-MM-DD format")
		}
		// Unix seconds avoid time.Duration's approximately 290-year limit.
		days = int((end.Unix() - start.Unix()) / 86400)
	} else {
		days = int(*in.Days)
		end = start.AddDate(0, 0, days)
		if end.Year() < 0 || end.Year() > 9999 {
			return nil, invalid("arguments.days", "result must remain between years 0000 and 9999")
		}
	}
	return map[string]any{"start": start.Format(time.DateOnly), "end": end.Format(time.DateOnly), "days": days, "weekday": end.Weekday().String()}, nil
}

type wordCount struct {
	Word  string `json:"word"`
	Count int    `json:"count"`
}

func analyzeText(raw json.RawMessage) (any, error) {
	var in struct {
		Text string `json:"text"`
	}
	decode(raw, &in)
	words := strings.Fields(in.Text)
	counts := map[string]int{}
	for _, word := range words {
		word = strings.ToLower(strings.TrimFunc(word, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }))
		if word != "" {
			counts[word]++
		}
	}
	frequent := make([]wordCount, 0, len(counts))
	for word, count := range counts {
		frequent = append(frequent, wordCount{word, count})
	}
	sort.Slice(frequent, func(i, j int) bool {
		if frequent[i].Count == frequent[j].Count {
			return frequent[i].Word < frequent[j].Word
		}
		return frequent[i].Count > frequent[j].Count
	})
	if len(frequent) > 10 {
		frequent = frequent[:10]
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(in.Text, "\r\n", "\n"), "\r", "\n")
	lineCount := 0
	if normalized != "" {
		lineCount = strings.Count(normalized, "\n") + 1
	}
	return map[string]any{"characters": utf8.RuneCountInString(in.Text), "words": len(words), "lines": lineCount, "reading_minutes": math.Ceil(float64(len(words)) / 200), "reading_words_per_minute": 200, "frequent_words": frequent}, nil
}
