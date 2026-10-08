package studyhost

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func worksheetFixture(count int) string {
	exercises := make([]map[string]any, count)
	for i := range exercises {
		exercises[i] = map[string]any{"prompt": "Exercise " + strconv.Itoa(i+1) + ": What is 2 + 3?", "answer": "5", "explanation": "Adding 2 and 3 gives 5."}
	}
	raw, _ := json.Marshal(map[string]any{"tool": "make_worksheet", "arguments": map[string]any{"title": "Arithmetic worksheet", "instructions": "Show your working.", "exercises": exercises}})
	return string(raw)
}
func TestWorksheetOriginIsolationAndNativeFixtures(t *testing.T) {
	for _, n := range []int{1, 3, 5, 30} {
		h, err := NewWorksheet(testToken)
		if err != nil {
			t.Fatal(err)
		}
		first := request(h, "POST", "/v1/tools/call", worksheetFixture(n), testToken)
		if first.Code != http.StatusOK {
			t.Fatal(n, first.Code)
		}
		second := request(h, "POST", "/v1/tools/call", worksheetFixture(n), testToken)
		if !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
			t.Fatal("replay changed")
		}
		var envelope map[string]json.RawMessage
		if json.Unmarshal(first.Body.Bytes(), &envelope) != nil {
			t.Fatal("bad response")
		}
		// Optional fixed synthetic fixtures exercise the actual native decoder/store.
		// Only test-owned output paths and constant educational data are used.
		if output := os.Getenv("WORKSHEET_TEST_OUTPUT"); output != "" {
			var call map[string]json.RawMessage
			_ = json.Unmarshal([]byte(worksheetFixture(n)), &call)
			fixture, _ := json.Marshal(map[string]json.RawMessage{"input": call["arguments"], "result": envelope["result"]})
			if err := os.MkdirAll(output, 0700); err != nil {
				t.Fatal("fixture directory")
			}
			if err := os.WriteFile(filepath.Join(output, "worksheet-"+strconv.Itoa(n)+".json"), fixture, 0600); err != nil {
				t.Fatal("fixture write")
			}
		}
		if request(h, "POST", "/v1/tools/call", fixture(1), testToken).Code != 400 {
			t.Fatal("worksheet process admitted quiz")
		}
		if request(handler(t), "POST", "/v1/tools/call", worksheetFixture(n), testToken).Code != 400 {
			t.Fatal("quiz process admitted worksheet")
		}
		if request(h, "POST", "/v1/tools/call", worksheetFixture(n), "").Code != 401 {
			t.Fatal("unauthed origin")
		}
	}
	for _, n := range []int{0, 31} {
		h, _ := NewWorksheet(testToken)
		if request(h, "POST", "/v1/tools/call", worksheetFixture(n), testToken).Code != 400 {
			t.Fatal("count bound")
		}
	}
}
