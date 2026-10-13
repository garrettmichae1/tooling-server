package tools

import (
	"fmt"
	"html"
	"strings"
)

func wordSearchHTML(result WordSearchResult, key bool) string {
	var b strings.Builder
	b.WriteString(wordSearchHTMLHead)
	name := "WORD SEARCH"
	if key {
		name = "ANSWER KEY"
	}
	fmt.Fprintf(&b, `<header><p class="brand">EDSGER · %s</p><h1>%s</h1><p class="subtitle">%d words · %s · %d × %d</p></header><main>`, name, html.EscapeString(result.Title), len(result.Words), html.EscapeString(result.Difficulty), result.Size, result.Size)
	if !key {
		b.WriteString(`<p class="instructions">Find each word in a straight line. Spaces, hyphens and apostrophes are omitted from the grid.</p>`)
	}
	highlight := make(map[WordSearchCell]bool)
	if key {
		for _, placement := range result.Placements {
			for _, cell := range placement.Cells {
				highlight[cell] = true
			}
		}
	}
	b.WriteString(`<table class="grid" aria-label="Word search letter grid"><tbody>`)
	for row, line := range result.Grid {
		b.WriteString(`<tr>`)
		for column, letter := range []byte(line) {
			class := ""
			if highlight[WordSearchCell{row, column}] {
				class = ` class="found"`
			}
			fmt.Fprintf(&b, `<td%s>%c</td>`, class, letter)
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</tbody></table><ul class="words">`)
	for _, word := range result.Words {
		fmt.Fprintf(&b, `<li>%s</li>`, html.EscapeString(word.Label))
	}
	b.WriteString(`</ul>`)
	if key {
		b.WriteString(`<ol class="coordinates">`)
		for _, p := range result.Placements {
			fmt.Fprintf(&b, `<li>%s: row %d, column %d → row %d, column %d (%s)</li>`, html.EscapeString(result.Words[p.WordID].Label), p.Start.Row+1, p.Start.Column+1, p.End.Row+1, p.End.Column+1, p.Direction)
		}
		b.WriteString(`</ol>`)
	}
	fmt.Fprintf(&b, `</main><footer>Each target has one verified location. Word meanings and theme are not independently verified.<br>Generator %s · Seed %d</footer></body></html>`, result.GeneratorVersion, result.Seed)
	return b.String()
}

const wordSearchHTMLHead = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none';"><title>Edsger word search</title><style>
@page{size:letter;margin:16mm}*{box-sizing:border-box}body{margin:0;padding:32px;background:#eef3fb;color:#152238;font:16px/1.5 system-ui,sans-serif}header,main,footer{max-width:760px;margin:auto}header{padding:26px;background:white;border-top:5px solid #276ae5;border-radius:14px}.brand{font-size:11px;font-weight:700;letter-spacing:.16em;color:#276ae5}h1{font-size:30px;line-height:1.2;margin:12px 0;overflow-wrap:anywhere;white-space:pre-wrap}.subtitle,.instructions{font-size:13px;color:#617085}main{margin-top:20px;padding:26px;background:white;border-radius:14px}.grid{width:100%;table-layout:fixed;border-collapse:collapse;font:700 clamp(10px,2.3vw,20px)/1.5 ui-monospace,monospace;text-align:center}.grid td{border:1px solid #dce5f2;padding:3px 0}.found{background:#dbeafe;color:#1349a7;box-shadow:inset 0 0 0 1px #276ae5}.words{list-style:none;padding:0;display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:8px 14px;font-size:13px}.words li{overflow-wrap:anywhere}.coordinates{font-size:11px;columns:2;padding-left:20px}.coordinates li{break-inside:avoid;overflow-wrap:anywhere}footer{font-size:10px;color:#617085;padding:20px 0}@media print{body{background:white;padding:0}header{padding:0 0 12px;border-radius:0}main{padding:0;border-radius:0}h1{font-size:24pt}.grid{font-size:12pt;break-inside:avoid}.grid td{padding:1px 0}.found{background:#dbeafe!important;print-color-adjust:exact;-webkit-print-color-adjust:exact}.words{font-size:10pt}.coordinates{font-size:8pt}footer{font-size:8pt}}
</style></head><body>`
