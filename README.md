# Edsger tooling server

A loopback Go service with **32 single-call utility tools**, plus the existing poster, PDF handout, chart, printable-object, and molecule tools. Utilities run in-process using the Go standard library: no paid model APIs, subprocesses, network requests, or persistent user data. They accept structured arguments rather than interpreting English.

This process is separate from the Edsger iPhone app and from the live model worker. It does not hold Apple receipts or a model key.

The user-facing names are **Make Poster** for a figure, **Make Handout** for a PDF page, **Create Chart** for a chart or graph, **Make Object** for a printable solid, and **Make Molecule** for a ball-and-stick model. A figure is a poster, icon, diagram, or chart. It does not generate photographs. A handout is a page a student can keep or print, including typeset math, chemistry formulas, and geometry figures. A chart is a graph with axes, on its own page. An object is an STL from an OpenSCAD script. A molecule is an STL built from a formula such as `H2O`.

## Run

For all 32 utilities, install Go 1.27 or newer and start without rendering binaries:

```sh
export FIGURE_TOKEN="$(openssl rand -base64 32)"
export FIGURE_TOOLS_ONLY=1
go run ./cmd/figureserver
```

The token must be 32 to 256 bytes without whitespace or control characters. Keep it in the trusted caller, outside model prompts and distributed phone-app credentials. The service binds to loopback. A phone/cloud integration needs a trusted authenticated gateway invoking these endpoints; this repository does not modify the Edsger app or model worker. Hosted use still needs connectivity and hosting resources.

For the legacy renderers too, unset `FIGURE_TOOLS_ONLY` (or set it to `0`) and follow the installation below. Default full mode retains strict binary checks. The OpenSCAD download script currently supports macOS only. Utilities run on supported Unix platforms without that download. Renderer paths are resolved before launching children in private scratch directories.

Install Rust, then build the pinned renderer and download the pinned typesetter:

```sh
sh scripts/fetch-vectorcraft.sh
sh scripts/fetch-typst.sh
sh scripts/fetch-openscad.sh
```

Start the server on loopback. The token must be at least 32 bytes and is read only from the environment:

```sh
export FIGURE_TOKEN="$(openssl rand -base64 32)"
export VECTORCRAFT_BIN="$PWD/third_party/vectorcraft/target/release/vectorcraft-cli"
export TYPST_BIN="$PWD/third_party/typst/typst"
export OPENSCAD_BIN="$PWD/third_party/openscad/openscad"
export PATH="$PWD/.toolchain/go/bin:$PATH"
go run ./cmd/figureserver
```

`FIGURE_BIND` defaults to `127.0.0.1:8787` and must be a loopback address.

## Agent contract

`GET /v1/quickstart` returns short [agent instructions](docs/QUICKSTART.md). `GET /v1/tools` returns names, descriptions, categories, JSON schemas, and runnable examples for all 32 utilities, plus configured legacy renderer availability. Filter with `?category=math`, `data`, `developer`, `writing`, `study`, `planning`, `visuals`, or `audio`. Fetch one definition with `GET /v1/tools/{name}`.

The trusted caller attaches `Authorization: Bearer $FIGURE_TOKEN` on every endpoint except health. A model emits only the tool name and arguments:

```json
{"tool":"calculate","arguments":{"expression":"(85 + 90 + 95) / 3"}}
```

Send that to `POST /v1/tools/call`. Alternatively, send the arguments object directly to `POST /v1/tools/calculate`. Both return:

```json
{"ok":true,"tool":"calculate","result":{"value":90,"precision":"float64","angle_unit":"radians"}}
```

Errors include `ok:false`, `code`, `field`, and a corrective `error`. Utility inputs reject unknown/duplicate object keys, null/wrong types, out-of-range numbers, excessive nesting, and trailing JSON. `maxLength` counts Unicode characters; `x-maxBytes` additionally caps UTF-8 bytes. HTTP 413 means the body limit was exceeded; 422 means a result cannot be returned within its limits. Utility results are capped at 1 MiB. Calendar/UUID tools use random identifiers; other utilities are deterministic for the same inputs and time-zone database.

See [docs/TOOLS.md](docs/TOOLS.md) for the complete list and model integration, and [docs/AUDIT_AND_ROADMAP.md](docs/AUDIT_AND_ROADMAP.md) for findings and expansion priorities.

`GET /v1/guide` returns [docs/TRANSCRIPTION.md](docs/TRANSCRIPTION.md) plus legacy composition directions. That contract covers Make Handout at `POST /v1/handouts`, Create Chart at `POST /v1/charts`, Make Object at `POST /v1/models`, and Make Molecule at `POST /v1/molecules`. Keep the larger art guide out of a small model's context unless it needs those renderers. Legacy endpoints and successful response formats are preserved.

## Verification

```sh
go test ./...
go test -race ./...
go vet ./...
GOMAXPROCS=2 go test ./internal/tools -run '^$' -fuzz FuzzToolCalls -fuzztime=5s
```

Tests exercise every catalog example through both HTTP call forms, known numerical results, exact rational arithmetic, malformed inputs, authentication/capacity, concurrency, escaping, encodings, WAV headers, and bounded output reads. Real renderer integration tests require the installed binaries and `go test -tags=integration ./...`; mock tests do not prove actual visual output.

## Limits

The session cap now includes pending startups. PDF/STL endpoints share two render slots in addition to the separate figure-session cap. Authentication runs before rate accounting; health checks and invalid credentials do not consume the 30-request budget. Capacity errors include `Retry-After`. Output file reads enforce caps before loading data and reject non-regular files. Private working directories and minimal environments are not OS-level CPU/memory/disk/network isolation.

Two sessions, 40 calls each, 90 seconds absolute, 30 seconds idle, 256 KB JSON, 8 MB PNG, 8 MB PDF, 30 requests a minute. The figure renderer is `vectorcraft-cli mcp --headless`. A handout is one `typst compile` of a server-owned template. A chart is one `typst compile` of a second template. An object is one `openscad` render of a script, returned as a binary STL of at most 200,000 triangles. Each runs in a private scratch directory with a minimal environment. The token is not passed to those processes. Typst is pinned to 0.15.1 with MiTeX 0.2.7, Whalogen 0.3.0, CeTZ 0.5.2, CeTZ Plot 0.1.4, and the packages those load (xarrow 0.3.1 and oxifmt 1.0.0) vendored beside the binary, system fonts ignored, and package lookup limited to that vendor tree. OpenSCAD is a pinned official macOS build. Callers cannot choose a binary, a shell command, or a file path.
