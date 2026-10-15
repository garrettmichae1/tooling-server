const HEAD = "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none';\"><title>Edsger word search</title><style>\n@page{size:letter;margin:16mm}*{box-sizing:border-box}body{margin:0;padding:32px;background:#eef3fb;color:#152238;font:16px/1.5 system-ui,sans-serif}header,main,footer{max-width:760px;margin:auto}header{padding:26px;background:white;border-top:5px solid #276ae5;border-radius:14px}.brand{font-size:11px;font-weight:700;letter-spacing:.16em;color:#276ae5}h1{font-size:30px;line-height:1.2;margin:12px 0;overflow-wrap:anywhere;white-space:pre-wrap}.subtitle,.instructions{font-size:13px;color:#617085}main{margin-top:20px;padding:26px;background:white;border-radius:14px}.grid{width:100%;table-layout:fixed;border-collapse:collapse;font:700 clamp(10px,2.3vw,20px)/1.5 ui-monospace,monospace;text-align:center}.grid td{border:1px solid #dce5f2;padding:3px 0}.found{background:#dbeafe;color:#1349a7;box-shadow:inset 0 0 0 1px #276ae5}.words{list-style:none;padding:0;display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:8px 14px;font-size:13px}.words li{overflow-wrap:anywhere}.coordinates{font-size:11px;columns:2;padding-left:20px}.coordinates li{break-inside:avoid;overflow-wrap:anywhere}footer{font-size:10px;color:#617085;padding:20px 0}@media print{body{background:white;padding:0}header{padding:0 0 12px;border-radius:0}main{padding:0;border-radius:0}h1{font-size:24pt}.grid{font-size:12pt;break-inside:avoid}.grid td{padding:1px 0}.found{background:#dbeafe!important;print-color-adjust:exact;-webkit-print-color-adjust:exact}.words{font-size:10pt}.coordinates{font-size:8pt}footer{font-size:8pt}}\n</style></head><body>";

// Pin the immutable generator head/CSS and accept only its inert body grammar.
// No HTML is executed at the edge; native clients render the verified grid.
export function safeWordSearchHTML(value: string): boolean {
  if (!value.startsWith(HEAD)) return false;
  const source = value.slice(HEAD.length), stack = ["html", "body"];
  const opening = new Map<string, string>([
    ['<header>', 'header'], ['<p class="brand">', 'p'], ['<h1>', 'h1'],
    ['<p class="subtitle">', 'p'], ['<main>', 'main'],
    ['<p class="instructions">', 'p'],
    ['<table class="grid" aria-label="Word search letter grid">', 'table'],
    ['<tbody>', 'tbody'], ['<tr>', 'tr'], ['<td>', 'td'],
    ['<td class="found">', 'td'], ['<ul class="words">', 'ul'],
    ['<li>', 'li'], ['<ol class="coordinates">', 'ol'], ['<footer>', 'footer'],
  ]);
  let position = 0;
  for (const part of source.matchAll(/<[^>]*>|[^<]+/g)) {
    if (part.index !== position) return false;
    const token = part[0]; position += token.length;
    if (!stack.length) return false;
    if (!token.startsWith("<")) continue;
    if (token === "<br>") continue;
    if (token.startsWith("</")) {
      if (token !== `</${stack.pop()}>`) return false;
    } else {
      const tag = opening.get(token);
      if (!tag) return false;
      stack.push(tag);
    }
  }
  return position === source.length && stack.length === 0;
}
