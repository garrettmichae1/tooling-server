# Tool catalog and model integration

All 39 utilities use the same authenticated API without subscription checks or model-provider credentials. No app or worker code is changed. Existing app/gateway policies still determine whether a particular user or model can reach this service.

## Tools

| Category | Tool | Result |
| --- | --- | --- |
| Math | `calculate` | Bounded arithmetic, powers, constants, named functions |
| Math | `convert_units` | 34 unit identifiers across seven dimensions |
| Math | `solve_quadratic` | Real/complex roots and degenerate cases |
| Math | `solve_linear_system` | Exact rational RREF, particular solution, nullspace basis, rank, pivots, onto/one-to-one |
| Math | `combinatorics` | Exact factorials, combinations, permutations |
| Math | `number_theory` | Exact GCD/LCM and prime factors |
| Developer | `convert_base` | Exact binary/octal/decimal/hex conversion |
| Data | `summarize_numbers` | Mean, median, range, population/sample deviation |
| Data | `linear_regression` | Slope, intercept, correlation, R-squared, RMSE |
| Data | `inspect_csv` | Counts, missingness, preview, numeric-cell statistics |
| Data | `normalize_csv` | Quoted CSV/TSV conversion |
| Writing | `analyze_text` | Unicode counts, frequent words, reading-time estimate |
| Writing | `compare_text` | Bounded line diff |
| Writing | `transform_text` | Case, whitespace, sorting, deduplication |
| Writing | `make_table` | CSV and Markdown table files |
| Study | `make_flashcards` | CSV flashcards and Markdown notes from supplied facts |
| Study | `grade_quiz` | Normalized string comparison against supplied key |
| Planning | `date_math` | Calendar-day addition/difference |
| Planning | `time_convert` | Explicit timestamp conversion with IANA DST rules |
| Planning | `make_calendar_event` | Single ICS event file, without scheduling |
| Planning | `make_checklist` | Markdown checklist from supplied tasks |
| Visuals | `make_diagram` | Simple top-to-bottom SVG flowchart |
| Visuals | `make_identicon` | Deterministic mirrored SVG avatar |
| Visuals | `color_contrast` | Opaque sRGB WCAG text-contrast flags |
| Visuals | `inspect_image` | PNG/JPEG/GIF dimensions and format from header |
| Audio | `make_tone` | Short sine-wave PCM WAV as base64 |
| Developer | `inspect_json` | Strict JSON formatting without lost integer precision |
| Developer | `test_regex` | Bounded RE2 matches, groups, byte offsets |
| Developer | `encode_text` | Base64/hex/URL-query text encoding |
| Developer | `hash_text` | SHA-256/SHA-512 checksums |
| Developer | `inspect_url` | URL components without fetching |
| Developer | `generate_uuid` | Random UUIDv4 identifiers |
| Study | `make_study_app` | Offline interactive quiz, explanations, retry missed, local progress/export |
| Study | `make_worksheet` | Written exercises, printable question sheet and separate worked answer key; no scripts |
| Data | `make_data_dashboard` | Offline interactive line/bar chart, toggles, summaries, exact table, CSV |
| Study | `make_sorting_lab` | Algorithm animation, single steps, server-computed trace/counters |
| Visuals | `make_pixel_art` | Scaled transparent PNG and SVG sprites from palette indices |
| Audio | `make_music_sequence` | Short WAV melodies/rests from MIDI pitches and beat durations |
| Planning | `plan_project` | Dependency schedule, critical tasks, slack, and CSV export |

Live schemas/examples are the source of truth: `GET /v1/tools`, filtered catalogs, or `GET /v1/tools/{name}`. No utility starts a figure session or calls an external process.

## Smaller model integration

The caller selects relevant definitions before inference. Fetch a category or specific definitions; do not paste all legacy art instructions or all 39 schemas into every prompt. Use the served quickstart for common instructions.

For native function calling, map each descriptor's `name`, `description`, and `input_schema` into the provider's function format. Server validation applies even if the provider ignores constraints. For a model without native function calling, request exactly this JSON shape and have the trusted caller parse it:

```json
{"tool":"solve_linear_system","arguments":{"matrix":[[1,1],[2,-1]],"constants":[5,1]}}
```

The trusted caller sends it to `POST /v1/tools/call`, then feeds `result` into the model's next turn. Authentication stays outside the prompt. Allow at most two argument repairs; respect retry headers and stop on authentication/capacity failures. Do not execute generated shell commands or evaluate generated code.

This is a REST function-call interface, not an MCP server endpoint or automatic integration with every model. VectorCraft still uses its internal stdio MCP client. Success with specific local models requires evaluation with the actual Edsger models after caller integration.

## Result interpretation

- Arithmetic, conversion, summary, regression, and quadratic roots return float64. They are not symbolic math or arbitrary-precision decimal finance. Extreme numeric ranges may be rejected. Quadratic intermediates use wider precision; `discriminant_sign` remains reliable when the approximate `discriminant` underflows to zero.
- Linear systems use bounded integer inputs and exact rational strings, such as `"1/3"`. Every consistent-system solution is the particular solution plus a linear combination of nullspace basis vectors. Columns are zero-based. Onto/one-to-one describe the coefficient matrix's map.
- CSV statistics use finite dot-decimal cells within +/-1e12. Missing/non-numeric cells are separate; partial numeric columns are not silently treated as wholly numeric. No causal inference is performed.
- Word counts and reading time use stated estimates. Quiz grading cannot judge equivalent prose or partial credit. Flashcard facts are not verified.
- SVG diagrams use supplied node order and simple routing. IDs are unique; edges connect distinct existing nodes; duplicate edges are rejected. Short labels give better results.
- Returned file text/base64 must be saved or previewed by the caller. Calendar/UUID IDs use cryptographic randomness. Calendar timestamps use whole seconds, explicit offsets, UTC output, escaped text, CRLF, and UTF-8-safe folding.
- Images are header-inspected without full validation. URLs are parsed without fetching or safety certification. Encodings/hashes are not encryption. Regex uses RE2 and caps output at 50 matches and 128 KiB of captured text.
- Time-zone conversion embeds Go zone data as a fallback; update the toolchain/database as civil time rules change.

## Interactive exports and integration

The quiz, dashboard, and sorting lab return complete HTML files with their data, styles, and server-owned scripts embedded. They work without network access after saving. The source data is escaped JSON; supplied text is displayed through DOM text nodes. A per-file script hash allows only the embedded script under its content security policy. No models generate executable code for these tools.

The caller must save/share the HTML or display it in an isolated web preview that permits scripts. Use a separate origin with no app credentials, or a sandbox permitting scripts/downloads without same-origin privileges. Browser restrictions may block local storage or file downloads; the quiz handles unavailable storage and still offers progress export. Progress belongs to that file/content version in that browser, not an Edsger account, and is not synced. Quiz answers remain visible in the file; it is practice, not secure assessment. Quiz correctness means matching the supplied answer key, whose facts still require review.

Dashboard summaries use float64 and charts show supplied categories in supplied order, with sparse/shortened axis labels; the table preserves exact serialized input values. All-negative and zero datasets are supported. CSV exports neutralize text starting with spreadsheet formula markers; numeric values remain numbers. Sorting counts comparisons of array values and array-slot writes; insertion frames can temporarily omit a held value, which the action text identifies. The full trace lives inside the HTML; the result also reports sorted values, counters, and frame count.

Pixel-art alpha is preserved in PNG and SVG. Palette indices describe the supplied grid; this is not image generation. Music uses equal-tempered MIDI pitch strings (48–96), mono sine waves at 16 kHz, and per-note fades. Beat endpoints are rounded cumulatively to samples to limit timing drift. Project planning assumes unlimited parallel workers and elapsed integer minutes, with no calendar or resource constraints. Critical tasks have zero slack; multiple critical paths can exist, so the returned IDs are not necessarily a single chain. It is a plan/export, not task execution.

## Adding tools

Define one concrete purpose, name, category, JSON schema, working example, and input/output caps in `internal/tools`. Implement deterministic in-process code where practical. Validate cross-field constraints and return a field-specific `*tools.Error`. Schemas must use supported validator keywords, or extend the validator and tests together.

Add known-answer, valid/invalid boundary, and round-trip tests where applicable. Catalog tests automatically invoke each example through both HTTP forms. Update catalog-count assertions and docs deliberately. Run normal/race tests, vet, and targeted fuzzing; evaluate actual models before claiming reliable model selection.

## Standards

- [RFC 5545: iCalendar](https://www.rfc-editor.org/rfc/rfc5545.html)
- [RFC 9562: UUIDs](https://www.rfc-editor.org/rfc/rfc9562.html)
- [W3C: WCAG text contrast](https://www.w3.org/WAI/WCAG21/Understanding/contrast-minimum.html)
- [Go regexp](https://pkg.go.dev/regexp)
