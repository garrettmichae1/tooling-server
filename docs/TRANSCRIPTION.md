# Figure transcription guide

For the 32 utility tools (math, data, writing, developer, study, planning, visuals, audio), start with `GET /v1/quickstart` and `GET /v1/tools?category=math`. Their JSON schemas, limits, and working examples are served by the catalog. They use `POST /v1/tools/call` and do not need a session or renderer. The rest of this document describes the legacy figure/PDF/STL tools.

This service draws a **figure**: a poster, icon, diagram, chart, or labeled vector drawing. It returns a PNG.

It does not generate photographs, painterly images, or pictures of people. If the user asks for a photo, a realistic scene, or "an image of" a person, do not call this service. Say that a figure can be a chart, diagram, icon, or poster instead.

You are the agent. You read the user's English prompt, decide the drawing, and send tool calls. The server does not interpret English. It only runs the calls below.

For a title card with a sky and a skyline, call `compose_poster` once, then export. Do not redraw that scene yourself. For a flyer, a cover, or any layout with more than a title and one subtitle, draw it with the low-level tools and follow [AGENT_DIRECTIONS.md](AGENT_DIRECTIONS.md). That file is the composition guide: palettes, baseline spacing, one glow, and when not to add another shape.

For a page the student can keep or print, call Make Handout. That is `POST /v1/handouts`, and the response is a PDF. Do not draw a worksheet with the figure tools.

For a chart or graph with axes, call Create Chart. That is `POST /v1/charts`, and the response is a PDF. Do not draw that chart with the figure tools.

For a shape the student can print on a 3D printer, call Make Object. That is `POST /v1/models`, and the response is an STL file. Do not draw a solid with the figure tools.

For a molecule the student can hold, call Make Molecule. That is `POST /v1/molecules` with a formula such as `H2O`. The response is a ball-and-stick STL. Do not write an OpenSCAD script for it.

## Connection

Base URL is whatever the operator bound, by default `http://127.0.0.1:8787`.

Every request except `GET /health` sends:

```
Authorization: Bearer <FIGURE_TOKEN>
```

The trusted caller attaches the token. Do not put it in a model prompt, query string, tool argument, or log line.

## Session

1. `POST /v1/sessions` with an empty body.
2. Repeat `POST /v1/sessions/{id}/calls` for each drawing step.
3. `POST /v1/sessions/{id}/export` with an empty body. The response is `image/png`. The session is then gone.
4. `DELETE /v1/sessions/{id}` if you need to stop without an image.

A session holds at most 40 calls, lives at most 90 seconds, and is closed after 30 seconds without a call. At most two sessions exist at once. The page is 612 by 792 points. The origin is the top-left. Y grows downward. You cannot change the page size.

### Create response

```json
{"id":"<32 hex chars>","expires_at":"<RFC3339>","tools":["add_text","draw_shape"]}
```

`tools` is the full allowlist. Ignore any other VectorCraft command you may know about.

### Call body

```json
{"tool":"draw_shape","arguments":{"shape":"ellipse","x":200,"y":160,"width":200,"height":200,"fill":"#ffb703"}}
```

Unknown JSON fields are rejected. Arguments must be one object.

### Call response

```json
{"ok":true,"result":{}}
```

`result` is the renderer reply with filesystem paths removed. A rejected call returns `422` and `{"error":"tool rejected the call"}`. The session stays open unless the renderer itself died. Adjust the next call. Do not retry the same arguments.

## Colors and text

Colors are `#rrggbb`. Text cannot contain `..`, a backslash, a null, or an absolute path. Keep each string under 8000 bytes.

## Poster

`compose_poster` takes `title`, optional `subtitle`, and optional `mood`. `mood` is `night` (the default), `dusk`, or `paper`.

```json
{"tool":"compose_poster","arguments":{"title":"CODE AFTER DARK","subtitle":"C  ·  PYTHON  ·  JS  ·  LUA","mood":"night"}}
```

Then export. One poster call is one step against the 40-call budget.

## Tools

`draw_shape` — `shape` is `rectangle`, `ellipse`, `polygon`, `star`, or `line`. Rectangles and ellipses take `x`, `y`, `width`, `height`. An optional `radius` rounds a rectangle. Polygons take `cx`, `cy`, `radius`, `sides`. Stars take `cx`, `cy`, `radius1`, `radius2`, `points`. Lines take `x1`, `y1`, `x2`, `y2`. Optional `fill`, `stroke`, `strokeWidth`.

`draw_path` — `points` is `[[x,y], ...]` or `d` is SVG path data. Optional `closed`, `fill`, `stroke`, `strokeWidth`.

`add_text` — `text`, and optional `x`, `y`, `width`, `height`, `size`, `font`, `color`.

`set_paint` — `fill` and optional `stroke`, `strokeWidth`. `fill` may be `#rrggbb` or `none`.

`create_graph` — `type` is `column`, `stackedColumn`, `bar`, `stackedBar`, `line`, `area`, `scatter`, `pie`, or `radar`. Also `x`, `y`, `width`, `height`. Data is `categories` plus `series`, or `rows`, or `csv`. Use one series of numbers for a simple chart, for example `"categories":["C","Python","JS","Lua"],"series":[4,7,5,3]`.

`pathfinder` — `operation` is `unite`, `minusFront`, `intersect`, `exclude`, `divide`, `trim`, `merge`, `crop`, `outline`, or `minusBack`.

`transform` — optional `dx`, `dy`, `rotate`, `scale`, `scaleX`, `scaleY`.

`apply_effect` — optional `effect` and `params`. Prefer shapes and type. Use an effect only when the poster needs one accent, such as a glow, and only with an effect id you already know. Do not invent a long effect stack.

`select_tool` — `tool` is a short tool name such as `rectangle` or `ellipse`.

`inspect_document` — no arguments. Use it when you need object ids before `pathfinder` or `transform`.

`list_commands` — no arguments. The server still only runs the tools in this guide.

`undo` and `redo` — no arguments.

These tools are rejected even if a menu lists them: `open_file`, `save_file`, `screenshot`, `pointer_gesture`, `type_text`, `press_key`, `inspect_ui`, `open_panel`, `run_command`, `export`. Export is the separate HTTP call. You cannot choose a file path.

## Make it look designed

Read [AGENT_DIRECTIONS.md](AGENT_DIRECTIONS.md) before inventing a layout. The rules that keep a figure from looking unfinished:

- Draw back to front: full-page field, one large shape, one focal shape, type last.
- Set `"stroke":"none"` on every poster shape. An omitted stroke draws an outline.
- `add_text` `y` is the baseline. The next line must sit at least `size + 16` below the previous baseline. A 72-point title at `y` 250 wants the next line at `y` 292 or lower.
- Use three to five colors from one palette in the art directions. One accent, used once.
- One glow, on the focal shape only, and only with a numeric `id` the renderer just returned. Effect id `stylize.outerGlow`, opacity 60 to 75, blur 14 to 20.
- One title of one to three words. No paragraph.
- Stop around 12 to 20 calls. The page gets worse if you spend the rest of the budget.

## Poster example

User prompt: "A night-drive poster, dark purple, a gold sun, mountains, and the title NIGHT DRIVE."

```json
{"tool":"draw_shape","arguments":{"shape":"rectangle","x":0,"y":0,"width":612,"height":792,"fill":"#12001c"}}
```

```json
{"tool":"draw_shape","arguments":{"shape":"ellipse","x":206,"y":150,"width":200,"height":200,"fill":"#ffb703"}}
```

```json
{"tool":"draw_path","arguments":{"points":[[0,520],[150,360],[270,500],[400,330],[520,480],[612,360],[612,792],[0,792]],"closed":true,"fill":"#2d0a4e"}}
```

```json
{"tool":"add_text","arguments":{"text":"NIGHT DRIVE","x":48,"y":72,"size":64,"color":"#ffffff"}}
```

Then `POST /v1/sessions/{id}/export`.

## Chart example

User prompt: "A column chart of study time for C, Python, JS, and Lua."

```json
{"tool":"draw_shape","arguments":{"shape":"rectangle","x":0,"y":0,"width":612,"height":792,"fill":"#0e1116"}}
```

```json
{"tool":"add_text","arguments":{"text":"Study hours","x":64,"y":80,"size":36,"color":"#f4f1ea"}}
```

```json
{"tool":"create_graph","arguments":{"type":"column","x":64,"y":180,"width":480,"height":360,"categories":["C","Python","JS","Lua"],"series":[4,7,5,3]}}
```

Then export. A chart that should stand on its own page is Create Chart, below, and not this recipe.

## Create Chart

`POST /v1/charts` with one JSON object. There is no session. The response is `application/pdf`, named `chart.pdf`. The server sets the page, the axes, and the colors. You send the numbers.

`title` is required. `type` is `column`, `bar`, `line`, `area`, `scatter`, or `pie`. `xlabel` and `ylabel` are optional.

Categories are the labels along one axis, at most 12. `series` is either one list of numbers, or up to four objects. An object has `name` and either `values` or `points`. `values` lines up with `categories`. `points` is `[[x, y], ...]`, and then you omit `categories`. A line or an area needs at least two positions. A scatter needs at least one. A pie needs one series and at least two categories.

`"stacked": true` is for a column or a bar with two or more named series. Stacked values and pie values are zero or greater. An area is zero or greater.

```json
{"title":"Study hours","type":"column","xlabel":"Language","ylabel":"Hours","categories":["C","Python","JS","Lua"],"series":[4,7,5,3]}
```

```json
{"title":"Cooling","type":"line","xlabel":"Minutes","ylabel":"Degrees","series":[{"name":"Beaker","points":[[0,80],[5,61],[10,47],[15,38]]}]}
```

```json
{"title":"Share of hours","type":"pie","categories":["C","Python","JS","Lua"],"series":[4,7,5,3]}
```

## Make Handout

`POST /v1/handouts` with one JSON object. There is no session. The response is `application/pdf`. The server sets the type, the margins, and the math. You send the words.

`title` is required. `kicker` is the subject label, such as Algebra, Chemistry, or C. `subtitle` is optional. `sections` is 1 to 8 groups. Each section has an optional `heading` and a `body` of blocks. At most 40 blocks on the page.

A `paragraph` has `parts`. A part is `{"text":"..."}`, `{"math":"..."}`, or `{"chem":"..."}`. Math is LaTeX, so a fraction is `\frac{a}{b}` and a square root is `\sqrt{x}`. Chemistry is a formula such as `H2O`. Use `math` or `chem` inside a sentence. Use a `math` block, with `latex`, when the equation should sit on its own line. Use a `reaction` block, with `formula`, when the reaction should sit on its own line, such as `HCl + H2O -> H3O+ + Cl-`.

A `figure` block draws a geometry diagram. It has `shapes`, at most 24. Coordinates are centimeters, each number from -20 to 20. A shape is `line` (`from`, `to`), `circle` (`at`, `radius`), `rect` (`at`, `width`, `height`), `polygon` (`points`), or `label` (`at`, `text`). Send the numbers. Do not send drawing-program source.

A `list` has `items`, and each item is a `parts` array. Set `"ordered": true` for steps. A `code` block has `lang` of `c`, `python`, `js`, or `lua`, and `text`. A `terms` block defines words: each item has `name` and `parts`. A `note` has a short `label` and `parts`.

Do not put `#`, `@`, or a backtick inside math. Do not use `\input`, `\include`, `\write`, or `\openout`. A chemistry formula may use `@` for an isotope. Do not put `#`, a backtick, a backslash, or `..` in a formula. Prose and code are shown as written, including a C `#include`.

### Algebra

```json
{"title":"The quadratic formula","kicker":"Algebra","subtitle":"One page for lab","sections":[{"heading":"The formula","body":[{"type":"paragraph","parts":[{"text":"For "},{"math":"ax^2 + bx + c = 0"},{"text":", the solutions are"}]},{"type":"math","latex":"x = \\frac{-b \\pm \\sqrt{b^2 - 4ac}}{2a}"},{"type":"note","label":"Remember","parts":[{"text":"A negative discriminant means no real roots."}]}]}]}
```

### Chemistry

```json
{"title":"Water","kicker":"Chemistry","sections":[{"heading":"The reaction","body":[{"type":"paragraph","parts":[{"text":"Water is "},{"chem":"H2O"},{"text":"."}]},{"type":"reaction","formula":"HCl + H2O -> H3O+ + Cl-"}]}]}
```

### Geometry

```json
{"title":"A right triangle","kicker":"Geometry","sections":[{"heading":"The sides","body":[{"type":"figure","shapes":[{"kind":"polygon","points":[[0,0],[4,0],[0,3]]},{"kind":"label","at":[2,-0.6],"text":"4 cm"},{"kind":"label","at":[-0.8,1.5],"text":"3 cm"},{"kind":"label","at":[2.2,1.6],"text":"5 cm"}]}]}]}
```

### C

```json
{"title":"Pointers","kicker":"C","sections":[{"heading":"Store an address","body":[{"type":"paragraph","parts":[{"text":"A pointer holds the address of another variable."}]},{"type":"code","lang":"c","text":"int x = 3;\nint *p = &x;\n"},{"type":"terms","items":[{"name":"pointer","parts":[{"text":"A variable that stores an address."}]}]}]}]}
```

## Make Object

`POST /v1/models` with one JSON object, `{"script":"..."}`. There is no session. The response is `model/stl`, named `model.stl`. The script is OpenSCAD: `cube`, `sphere`, `cylinder`, `translate`, `difference`, and loops. A 20 mm cube with a hole is:

```json
{"script":"difference() {\n  cube(20);\n  translate([10, 10, -1]) cylinder(h=22, r=6);\n}"}
```

Do not use `include`, `use`, `import`, or `surface`. Do not read a file path. Keep the script under 16 KB.

## Make Molecule

`POST /v1/molecules` with `{"formula":"H2O"}`. There is no session. The response is `model/stl`, named `molecule.stl`. Hydrogen balls are smaller than the other atoms. A rod joins each bond.

Send the formula, not a script. These formulas have a model: `H2`, `N2`, `O2`, `HF`, `HCl`, `H2O`, `H2O2`, `CO2`, `NH3`, `CH4`, `SO2`, `CH4O` (methanol, also `CH3OH`), `C2H6`, `C2H4`, `C2H2`, `C6H6`, and `H2SO4`. Element order does not matter. A formula with no model is rejected.

## Errors

| HTTP | Meaning |
| --- | --- |
| 400 | The body, tool, or arguments were rejected, or the call budget is spent. |
| 401 | Missing or wrong bearer token. |
| 404 | Unknown session. |
| 422 | The renderer rejected the call, the export was not a PNG, the handout did not compile, the chart did not compile, or the object did not render. |
| 429 | Too many requests, or two sessions are already running. |
| 503 | The renderer did not start. |

`GET /health` needs no token and returns `{"status":"ok"}`. `GET /v1/guide` returns this document.
