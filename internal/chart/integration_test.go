//go:build integration

package chart

import (
	"context"
	"os"
	"testing"
)

func TestRealCharts(t *testing.T) {
	bin := os.Getenv("TYPST_BIN")
	if bin == "" {
		t.Skip("TYPST_BIN is not set")
	}
	c := Compiler{Bin: bin, MaxPDF: 8 * 1024 * 1024}
	cases := []string{
		`{"title":"Study hours","type":"column","xlabel":"Language","ylabel":"Hours","categories":["C","Python","JS","Lua"],"series":[4,7,5,3]}`,
		`{"title":"Study hours","type":"column","xlabel":"Language","ylabel":"Hours","stacked":true,"categories":["C","Python","JS","Lua"],"series":[{"name":"Lab","values":[4,7,5,3]},{"name":"Home","values":[1,2,2,1]}]}`,
		`{"title":"Study hours","type":"bar","xlabel":"Hours","ylabel":"Language","categories":["C","Python","JS","Lua"],"series":[4,7,5,3]}`,
		`{"title":"Cooling","type":"line","xlabel":"Minutes","ylabel":"Degrees","series":[{"name":"Beaker","points":[[0,80],[5,61],[10,47],[15,38]]}]}`,
		`{"title":"Scores","type":"line","ylabel":"Score","categories":["Quiz 1","Quiz 2","Quiz 3"],"series":[{"name":"Section A","values":[7,8,9]},{"name":"Section B","values":[6,7,8]}]}`,
		`{"title":"Hours by day","type":"area","ylabel":"Hours","categories":["Mon","Tue","Wed"],"series":[2,4,3]}`,
		`{"title":"Trials","type":"scatter","xlabel":"Mass","ylabel":"Time","series":[{"name":"A","points":[[1,2],[2,2.4],[3,3.1]]},{"name":"B","points":[[1,1.5],[2,1.8],[3,2.2]]}]}`,
		`{"title":"Share of hours","type":"pie","categories":["C","Python","JS","Lua"],"series":[4,7,5,3]}`,
		`{"title":"Change","type":"column","ylabel":"Change","categories":["A","B","C"],"series":[-2,3,-1]}`,
	}
	for _, raw := range cases {
		pdf, err := c.Compile(context.Background(), []byte(raw))
		if err != nil {
			t.Fatalf("compile %s: %v", raw, err)
		}
		if len(pdf) < 1000 || string(pdf[:5]) != "%PDF-" {
			t.Fatalf("pdf %d bytes", len(pdf))
		}
	}
}
