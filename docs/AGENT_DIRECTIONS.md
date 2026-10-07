# Figure art directions

Give these directions to an agent when it needs the legacy renderers. The trusted caller attaches the bearer token outside the model prompt. The agent draws. The server does not read the user's sentence. For utility tools, use the short guide at `GET /v1/quickstart` instead.

This makes a designed figure: a poster, flyer, icon, or chart. It does not make a photograph, a face, or a painted scene. If the user asks for those, say so and offer a poster instead.

## Call the server

Base URL is `http://127.0.0.1:8787` unless the operator says otherwise.

Every request except `GET /health` sends `Authorization: Bearer <FIGURE_TOKEN>`. Never put the token in a tool argument, a query string, or a log.

1. `POST /v1/sessions` with an empty body. Keep `id`.
2. `POST /v1/sessions/{id}/calls` with `{"tool","arguments"}` for each step.
3. `POST /v1/sessions/{id}/export` with an empty body. The body is a PNG. The session is then gone.

Plan the whole picture before the first call. A session allows 40 calls, 90 seconds total, and 30 seconds of silence. The HTTP limit is 30 requests a minute, and the session, every call, and the export all count. One figure should be about 12 to 20 calls. If you get `429`, wait and do not start a second session.

The page is 612 by 792. The origin is the top-left. Y grows downward. You cannot change the page size.

A `422` means that call was refused. Change the arguments. Do not send the same call again. Paths, `..`, backslashes, and absolute paths are rejected in any string.

`GET /v1/guide` is the tool contract. If it disagrees with this file about a parameter, follow the guide. Follow this file for composition.

## Decide in one step

| The user wants | What you send |
| --- | --- |
| A title card with a sky, a moon, and a skyline | One `compose_poster`, then export. Do not draw the sky yourself. |
| A flyer, cover, diagram, or anything with more than a title and one subtitle | Draw it yourself. Do not call `compose_poster`. |
| A chart drawn on a poster | A field, one title, one `create_graph`, then export. |
| A chart or graph on its own page | One `POST /v1/charts`. Do not draw it. |
| A study page, worksheet, or anything they would print | One `POST /v1/handouts`. Do not draw it. |
| A shape they can print on a 3D printer | One `POST /v1/models`. Do not draw it. |
| A molecule they can hold | One `POST /v1/molecules` with the formula. Do not write a script for it. |

`compose_poster` arguments are `title`, optional `subtitle`, and optional `mood`: `night`, `dusk`, or `paper`.

```json
{"tool":"compose_poster","arguments":{"title":"CODE AFTER DARK","subtitle":"C  ·  PYTHON  ·  JS  ·  LUA","mood":"night"}}
```

That call is one step. It already sets no outlines, a halo, stars, a glow, and type with room around it.

## Create Chart

A chart is a graph with axes, not a poster. One request returns a PDF. `POST /v1/charts` with `title`, `type`, optional `xlabel` and `ylabel`, and the data.

`type` is `column`, `bar`, `line`, `area`, `scatter`, or `pie`. One series can be a list of numbers next to `categories`. Several series are objects with `name` and `values`. A line through measured points uses `points` and no categories. `"stacked": true` stacks a column or a bar.

```json
{"title":"Study hours","type":"column","xlabel":"Language","ylabel":"Hours","categories":["C","Python","JS","Lua"],"series":[4,7,5,3]}
```

```json
{"title":"Cooling","type":"line","xlabel":"Minutes","ylabel":"Degrees","series":[{"name":"Beaker","points":[[0,80],[5,61],[10,47],[15,38]]}]}
```

A chart that belongs on a poster still uses `create_graph`. Create Chart is the page by itself.

## Make Handout

A handout is a page, not a poster. One request returns a PDF. The same blocks cover algebra, chemistry, a coding lesson, or a page of notes. The kicker is the subject. There is no theme switch.

`POST /v1/handouts` with `title`, optional `kicker` and `subtitle`, and `sections`. Each section has an optional `heading` and a `body`.

- `paragraph` — `parts` of `{"text"}`, `{"math"}`, and `{"chem"}`. Math inside a sentence uses `math`. A formula inside a sentence uses `chem`, such as `H2O`.
- `math` — one display equation. The field is `latex`.
- `reaction` — one centered reaction. The field is `formula`, such as `HCl + H2O -> H3O+ + Cl-`.
- `figure` — up to 24 shapes. Coordinates are centimeters, from -20 to 20. Kinds are `line` (`from`, `to`), `circle` (`at`, `radius`), `rect` (`at`, `width`, `height`), `polygon` (`points`), and `label` (`at`, `text`).
- `list` — `items` of `parts` arrays. `"ordered": true` numbers the steps.
- `code` — `lang` is `c`, `python`, `js`, or `lua`.
- `terms` — each item has `name` and `parts`.
- `note` — a short `label` and `parts`, for one rule to remember.

Math is LaTeX: `\frac{a}{b}`, `\sqrt{x}`, `\sum_{i=1}^{n}`. Keep `#`, `@`, and backticks out of math. A chemistry formula may use `@` for an isotope, and must not contain `#`, a backtick, a backslash, or `..`. A C `#include` belongs in a code block, where it is shown as source.

Use one display equation when the formula is the point. Put short formulas in the sentence. A page needs a title, one or two sections, and then stop. Do not fill it to the 40-block limit.

```json
{"title":"The quadratic formula","kicker":"Algebra","sections":[{"heading":"The formula","body":[{"type":"paragraph","parts":[{"text":"For "},{"math":"ax^2 + bx + c = 0"},{"text":", the solutions are"}]},{"type":"math","latex":"x = \\frac{-b \\pm \\sqrt{b^2 - 4ac}}{2a}"}]}]}
```

```json
{"title":"Water","kicker":"Chemistry","sections":[{"body":[{"type":"paragraph","parts":[{"text":"Water is "},{"chem":"H2O"},{"text":"."}]},{"type":"reaction","formula":"HCl + H2O -> H3O+ + Cl-"}]}]}
```

```json
{"title":"A right triangle","kicker":"Geometry","sections":[{"body":[{"type":"figure","shapes":[{"kind":"polygon","points":[[0,0],[4,0],[0,3]]},{"kind":"label","at":[2,-0.6],"text":"4 cm"},{"kind":"label","at":[-0.8,1.5],"text":"3 cm"},{"kind":"label","at":[2.2,1.6],"text":"5 cm"}]}]}]}
```

```json
{"title":"Pointers","kicker":"C","sections":[{"heading":"Store an address","body":[{"type":"code","lang":"c","text":"int x = 3;\nint *p = &x;\n"},{"type":"terms","items":[{"name":"pointer","parts":[{"text":"A variable that stores an address."}]}]}]}]}
```

## Make Object

An object is a solid, not a poster and not a page. One request returns an STL a printer can use.

`POST /v1/models` with `{"script":"..."}`. The script is OpenSCAD. Units are millimeters. A cube with a hole:

```json
{"script":"difference() {\n  cube(20);\n  translate([10, 10, -1]) cylinder(h=22, r=6);\n}"}
```

Keep the script under 16 KB. Do not use `include`, `use`, `import`, or `surface`, and do not name a file path. The server renders the script. It does not turn an English description into a solid.

## Make Molecule

A molecule is a ball-and-stick model of a formula, not a script. One request returns an STL.

`POST /v1/molecules` with `{"formula":"H2O"}`. Hydrogen is the small ball. The other atoms are larger, and a rod is the bond. Water is bent. Carbon dioxide is straight. Methane is a tetrahedron.

The formulas with a model are `H2`, `N2`, `O2`, `HF`, `HCl`, `H2O`, `H2O2`, `CO2`, `NH3`, `CH4`, `SO2`, `CH4O`, `C2H6`, `C2H4`, `C2H2`, `C6H6`, and `H2SO4`. `CH3OH` is methanol, the same model as `CH4O`. If the formula is not in that list, say so. Do not invent a script to fake the molecule.

## Draw back to front

1. A full-page rectangle, so the page is not white.
2. One large shape, usually a circle that runs off a corner.
3. One focal shape, smaller than the large shape.
4. At most eight tiny marks (stars, dots).
5. At most one card.
6. Type last, so it sits on top.
7. At most one glow, on the focal shape only.
8. Export.

Leave the call budget unused. Extra shapes make the picture worse.

## Color

Use three to five colors from one row. Do not mix rows.

| Mood | Field | Second shape | Paper | Warm | Accent |
| --- | --- | --- | --- | --- | --- |
| Night | `#0c1220` | `#1a2744` | `#f7f4ee` | `#f6e7c1` | `#8eb4ff` |
| Dusk | `#1a1030` | `#5a2a48` | `#fff6ef` | `#ffb085` | `#ffd2a8` |
| Day paper | `#f4f0e6` | `#e7e0d2` | `#fffdf8` | `#1c1916` | `#6b5344` |

The field is the largest area. Paper is for the title. Warm is for one line or the focal disc. Accent is for a single short line, not the title.

Every shape sets `"stroke":"none"`. An omitted stroke draws an outline and the figure looks unfinished. A line in a diagram may use a stroke. A poster may not.

A gradient is allowed as `fill` on one shape, usually the field:

```json
{"shape":"rectangle","x":0,"y":0,"width":612,"height":792,"stroke":"none","fill":{"gradient":{"kind":"linear","angle":160,"stops":[{"offset":0,"color":"#0c1220"},{"offset":1,"color":"#1a2744"}]}}}
```

`kind` is `linear` or `radial`. Two stops are enough. If that call returns `422`, use a solid field and a second ellipse. Do not retry the same gradient.

## Type

`y` on `add_text` is the baseline, not the top of the letters. A line of size `S` occupies about `y - 0.8S` through `y`. The next baseline must be at least `S + 16` below the previous one. A small line placed 20 points under a 72-point title lands inside the title.

Margins are 48 on the left and 48 on the right, so text lives between x = 48 and x = 564.

| Role | Size | Words |
| --- | --- | --- |
| Title | 56 to 78 | 1 to 3 |
| Card line | 22 to 28 | 2 to 4 |
| Footer | 14 to 16 | one line |

Use at most six lines on the page. Do not wrap a paragraph. Point text does not wrap. If a line needs a box, pass `width` and `height` and keep the copy to two short lines.

Fonts that ship with the renderer: `Inter`, `Source Sans 3`, `Source Serif 4`, `JetBrains Mono`. Titles use `Inter`. A code footer may use `JetBrains Mono`. If a font is rejected, omit `font`.

```json
{"tool":"add_text","arguments":{"text":"EDSGER","x":48,"y":250,"size":72,"font":"Inter","color":"#f7f4ee"}}
```

The next line, if size 16, goes at `y` 292 or lower. Not at 270.

Titles are short and usually capitals. Separate items with a spaced middle dot: `C  ·  PYTHON  ·  JS  ·  LUA`. No string may contain `..`, a backslash, or an absolute path.

## One glow

Draw the focal disc. The call result includes a numeric `id` inside a text block shaped like `{"id":4}`. Then:

```json
{"tool":"apply_effect","arguments":{"effect":"stylize.outerGlow","params":{"color":"#f4e4b2","opacity":70,"blur":18},"ids":[4]}}
```

Use the id you were given. Opacity stays between 60 and 75. Blur stays between 14 and 20. One glow on the page. If there is no numeric id, skip the glow.

A card may take a shadow instead of a second glow, and only if nothing else glows:

```json
{"tool":"apply_effect","arguments":{"effect":"stylize.dropShadow","params":{"x":0,"y":10,"blur":18,"color":"#000000","opacity":40},"ids":[8]}}
```

## Recipes

### Corner light

Field, a circle bleeding off the top-right, a warm disc inside that circle, four to six dots of paper color at 2 or 3 points wide, the title in the open left side, one line under it.

```json
{"tool":"draw_shape","arguments":{"shape":"rectangle","x":0,"y":0,"width":612,"height":792,"fill":"#0c1220","stroke":"none"}}
```

```json
{"tool":"draw_shape","arguments":{"shape":"ellipse","x":280,"y":-90,"width":420,"height":420,"fill":"#1a2744","stroke":"none"}}
```

```json
{"tool":"draw_shape","arguments":{"shape":"ellipse","x":400,"y":48,"width":150,"height":150,"fill":"#f6e7c1","stroke":"none"}}
```

Glow that disc. Then the title at `x` 48, `y` 250, size 72. Then one subtitle at `y` 300, size 16, accent color.

### Flyer card

Same field and corner light. Title above the card, with the baseline gap from the type section. One rounded rectangle. Three or four lines inside it, inset 28 points from the card's left and top. The card's first baseline is about 70 points below the card's top, because type hangs above its baseline.

```json
{"tool":"draw_shape","arguments":{"shape":"rectangle","x":48,"y":360,"width":516,"height":230,"radius":18,"fill":"#141c30","stroke":"none"}}
```

Do not let a line cross the card edge or the disc. Footer sits near `y` 680, size 16, and a second footer no sooner than `y` 712.

### Paper poster

Day-paper colors. Field `#f4f0e6`. A large circle `#e7e0d2` off the top-left. Title in `#1c1916`. One accent line in `#6b5344`. No stars. No glow, or a very soft shadow on a white card `#fffdf8`.

### Chart

```json
{"tool":"draw_shape","arguments":{"shape":"rectangle","x":0,"y":0,"width":612,"height":792,"fill":"#0c1220","stroke":"none"}}
```

```json
{"tool":"add_text","arguments":{"text":"STUDY HOURS","x":64,"y":120,"size":36,"font":"Inter","color":"#f7f4ee"}}
```

```json
{"tool":"create_graph","arguments":{"type":"column","x":64,"y":180,"width":480,"height":420,"categories":["C","Python","JS","Lua"],"series":[4,7,5,3]}}
```

No moon, no card, no second chart. If the chart is the whole request, use Create Chart instead of this recipe.

## Marks that stay quiet

Stars are ellipses 2 or 3 points wide, in the paper color, scattered, not in a row. A five-point star is one emblem, not a sky: `shape` `star`, `radius1` about 28, `radius2` about 12, `points` 5, fill only.

A ridge or skyline, when you are not using `compose_poster`, is one closed path with uneven peaks and `"stroke":"none"`. Do not stack rectangles into a city unless the user asked for buildings. Windows, if you draw them, are few, different per building, and not a full grid.

Rounded rectangles use `radius` 16 to 20. Do not round the full-page field.

## Before you export

Check the picture in your head against this list:

- The field covers the page.
- Nothing has an outline unless it is a diagram line.
- The title is one line and does not touch the focal shape.
- No two lines share a baseline band.
- There is one accent color, used once.
- There is one glow, or none.
- The copy would still make sense if a word were removed.

Then export. Look at the PNG. If type collides, open a new session and move the baselines. Do not pile `undo` calls until the budget is gone.

## Do not call

`open_file`, `save_file`, `screenshot`, `pointer_gesture`, `type_text`, `press_key`, `inspect_ui`, `open_panel`, `run_command`, and `export` as a tool. Export is the HTTP call. You cannot choose a file path. `list_commands` does not add tools.
