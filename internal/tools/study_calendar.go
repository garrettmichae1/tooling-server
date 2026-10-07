package tools

import (
	"bytes"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func makeFlashcards(raw json.RawMessage) (any, error) {
	var in struct {
		Title string
		Cards []struct{ Question, Answer string }
	}
	decode(raw, &in)
	if strings.TrimSpace(in.Title) == "" {
		return nil, invalid("arguments.title", "title cannot be blank")
	}
	var out bytes.Buffer
	writer := csv.NewWriter(&out)
	_ = writer.Write([]string{"question", "answer"})
	var md strings.Builder
	fmt.Fprintf(&md, "# %s\n\n", markdownText(in.Title))
	for i, c := range in.Cards {
		if strings.TrimSpace(c.Question) == "" || strings.TrimSpace(c.Answer) == "" {
			return nil, invalid("arguments.cards", "questions and answers must contain text")
		}
		_ = writer.Write([]string{c.Question, c.Answer})
		fmt.Fprintf(&md, "## %d. %s\n\n%s\n\n", i+1, markdownText(c.Question), markdownText(c.Answer))
	}
	writer.Flush()
	if writer.Error() != nil {
		return nil, invalid("arguments.cards", "could not create CSV")
	}
	return map[string]any{"count": len(in.Cards), "files": []map[string]string{{"filename": "flashcards.csv", "media_type": "text/csv; charset=utf-8", "text": out.String()}, {"filename": "study_notes.md", "media_type": "text/markdown; charset=utf-8", "text": md.String()}}, "facts_verified": false}, nil
}

func markdownText(text string) string {
	// Keep supplied text as text, rather than executable HTML or Markdown links.
	r := strings.NewReplacer("\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "#", "\\#", "!", "\\!", "|", "\\|")
	return r.Replace(strings.Join(strings.Fields(text), " "))
}

func gradeQuiz(raw json.RawMessage) (any, error) {
	var in struct{ Answers, Key []string }
	decode(raw, &in)
	if len(in.Answers) != len(in.Key) {
		return nil, invalid("arguments.key", "answer and key arrays must have equal length")
	}
	normalize := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	results := make([]map[string]any, len(in.Key))
	correct := 0
	for i, key := range in.Key {
		if normalize(key) == "" {
			return nil, invalid("arguments.key", "answer key entries cannot be blank")
		}
		ok := strings.EqualFold(normalize(in.Answers[i]), normalize(key))
		if ok {
			correct++
		}
		results[i] = map[string]any{"question": i + 1, "correct": ok, "answer": in.Answers[i], "expected": key}
	}
	return map[string]any{"correct": correct, "total": len(in.Key), "percentage": 100 * float64(correct) / float64(len(in.Key)), "results": results, "grading": "normalized_string_equality"}, nil
}

func makeCalendarEvent(raw json.RawMessage) (any, error) {
	var in struct{ Title, Start, End, Description, Location string }
	decode(raw, &in)
	start, err := parseTimestamp(in.Start)
	if err != nil {
		return nil, invalid("arguments.start", "use RFC3339 with an explicit UTC offset")
	}
	end, err := parseTimestamp(in.End)
	if err != nil || !end.After(start) {
		return nil, invalid("arguments.end", "end must be a valid RFC3339 time after start")
	}
	start, end = start.UTC(), end.UTC()
	if start.Year() < 1 || end.Year() > 9999 {
		return nil, invalid("arguments", "UTC dates must remain between years 0001 and 9999")
	}
	if start.Nanosecond() != 0 || end.Nanosecond() != 0 {
		return nil, invalid("arguments", "calendar times must use whole seconds")
	}
	for _, s := range []string{in.Title, in.Description, in.Location} {
		if strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) >= 0 {
			return nil, invalid("arguments", "calendar text contains unsupported control characters")
		}
	}
	if strings.TrimSpace(in.Title) == "" {
		return nil, invalid("arguments.title", "event title cannot be blank")
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, &Error{Code: "tool_failed", Message: "could not generate event id"}
	}
	uid := hex.EncodeToString(id[:]) + "@edsger.local"
	stamp := time.Now().UTC().Format("20060102T150405Z")
	content := []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Edsger//Tooling Server//EN", "CALSCALE:GREGORIAN", "BEGIN:VEVENT", "UID:" + uid, "DTSTAMP:" + stamp, "DTSTART:" + start.Format("20060102T150405Z"), "DTEND:" + end.Format("20060102T150405Z"), "SUMMARY:" + calendarEscape(in.Title)}
	if in.Description != "" {
		content = append(content, "DESCRIPTION:"+calendarEscape(in.Description))
	}
	if in.Location != "" {
		content = append(content, "LOCATION:"+calendarEscape(in.Location))
	}
	content = append(content, "END:VEVENT", "END:VCALENDAR")
	var out strings.Builder
	for _, line := range content {
		out.WriteString(foldCalendarLine(line))
		out.WriteString("\r\n")
	}
	return map[string]any{"filename": "event.ics", "media_type": "text/calendar; charset=utf-8", "text": out.String(), "uid": uid, "start_utc": start.Format(time.RFC3339), "end_utc": end.Format(time.RFC3339)}, nil
}

func calendarEscape(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	return strings.NewReplacer("\\", "\\\\", ";", "\\;", ",", "\\,", "\n", "\\n").Replace(s)
}

// RFC 5545 folds at 75 octets, without splitting a UTF-8 code point.
func foldCalendarLine(s string) string {
	var out strings.Builder
	limit := 75
	for len(s) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		out.WriteString(s[:cut])
		out.WriteString("\r\n ")
		s = s[cut:]
		limit = 74
	}
	out.WriteString(s)
	return out.String()
}
