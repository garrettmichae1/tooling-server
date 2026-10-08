package tools

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"
)

// A worksheet is data, never model-authored HTML or executable typesetting.
// The two exports intentionally keep the answer key out of the question sheet.
func makeWorksheet(raw json.RawMessage) (any, error) {
	var in struct {
		Title        string `json:"title"`
		Instructions string `json:"instructions"`
		Exercises    []struct {
			Prompt      string `json:"prompt"`
			Answer      string `json:"answer"`
			Explanation string `json:"explanation"`
		} `json:"exercises"`
	}
	decode(raw, &in)
	if err := nonblank(in.Title, "arguments.title"); err != nil {
		return nil, err
	}
	if err := nonblank(in.Instructions, "arguments.instructions"); err != nil {
		return nil, err
	}
	for _, e := range in.Exercises {
		for _, value := range []struct{ text, field string }{{e.Prompt, "prompt"}, {e.Answer, "answer"}, {e.Explanation, "explanation"}} {
			if err := nonblank(value.text, "arguments.exercises."+value.field); err != nil {
				return nil, err
			}
		}
	}
	build := func(key bool) string {
		var b strings.Builder
		b.WriteString(worksheetHead)
		fmt.Fprintf(&b, `<header><p class="brand">EDSGER · %s</p><h1>%s</h1><p class="intro">%s</p>`, map[bool]string{false: "WORKSHEET", true: "ANSWER KEY"}[key], html.EscapeString(in.Title), html.EscapeString(in.Instructions))
		if !key {
			b.WriteString(`<p class="identity">Name ________________________ &nbsp; Date ______________</p>`)
		}
		b.WriteString(`</header><main>`)
		for i, e := range in.Exercises {
			fmt.Fprintf(&b, `<section><h2>Exercise %02d</h2><p class="prompt">%s</p>`, i+1, html.EscapeString(e.Prompt))
			if key {
				fmt.Fprintf(&b, `<div class="solution"><h3>Suggested answer</h3><p>%s</p><h3>How to approach it</h3><p>%s</p></div>`, html.EscapeString(e.Answer), html.EscapeString(e.Explanation))
			} else {
				b.WriteString(`<div class="work" aria-label="Space for your answer"><div></div><div></div><div></div><div></div></div>`)
			}
			b.WriteString(`</section>`)
		}
		b.WriteString(`</main><footer>AI-generated practice. Review important answers against your learning material.</footer></body></html>`)
		return b.String()
	}
	sheet, key := build(false), build(true)
	result := map[string]any{"filename": "worksheet.html", "media_type": "text/html; charset=utf-8", "html": sheet, "answer_key_html": key, "offline": true, "network_requests": false, "preview_requires_scripts": false, "content_verified": false}
	encoded, err := json.Marshal(result)
	if err != nil || len(sheet) > 524_288 || len(key) > 524_288 || len(encoded) > 1_099_800 {
		return nil, &Error{Code: "result_too_large", Message: "worksheet exceeds output limits"}
	}
	return result, nil
}

const worksheetHead = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none';"><title>Edsger worksheet</title><style>
@page{size:letter;margin:18mm}*{box-sizing:border-box}body{font:16px/1.6 system-ui,sans-serif;color:#152238;background:#eef3fb;margin:0;padding:32px}header,main,footer{max-width:760px;margin:auto}header{padding:30px;background:#fff;border-top:5px solid #276ae5;border-radius:14px}.brand{font-size:11px;font-weight:700;letter-spacing:.18em;color:#276ae5}h1{font-size:32px;line-height:1.2;margin:12px 0}.intro,.prompt,.solution p{white-space:pre-wrap;overflow-wrap:anywhere}.identity{font-size:12px;color:#617085;margin-top:24px}section{margin-top:22px;padding:28px;background:#fff;border:1px solid #dce5f2;border-radius:14px;break-inside:avoid}h2,h3{font-size:12px;letter-spacing:.08em;text-transform:uppercase;color:#276ae5}h3{margin-top:18px}.prompt{font-size:17px}.work{margin-top:22px}.work div{height:30px;border-bottom:1px solid #dce5f2}.solution{border-left:3px solid #276ae5;padding-left:18px}footer{font-size:11px;color:#617085;padding:26px 0}@media print{body{padding:0;background:#fff;font-size:11pt}header{padding:0 0 16px;border-radius:0}h1{font-size:25pt}section{padding:15px 0;border:0;border-bottom:1px solid #dce5f2;border-radius:0}.prompt{font-size:11pt}.work div{height:27px}footer{font-size:8pt}}
</style></head><body>`
