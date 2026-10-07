# Edsger figure server

A localhost Go service that draws figures and charts, typesets handouts, and renders printable objects. An agent reads the transcription guide, sends a tool call, and receives a PNG, a PDF, or an STL.

This process is separate from the Edsger iPhone app and from the live model worker. It does not hold Apple receipts or a model key.

The user-facing names are **Make Poster** for a figure, **Make Handout** for a PDF page, **Create Chart** for a chart or graph, **Make Object** for a printable solid, and **Make Molecule** for a ball-and-stick model. A figure is a poster, icon, diagram, or chart. It does not generate photographs. A handout is a page a student can keep or print, including typeset math, chemistry formulas, and geometry figures. A chart is a graph with axes, on its own page. An object is an STL from an OpenSCAD script. A molecule is an STL built from a formula such as `H2O`.

## Run

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

`GET /v1/guide` with `Authorization: Bearer $FIGURE_TOKEN` returns [docs/TRANSCRIPTION.md](docs/TRANSCRIPTION.md). That document is the tool contract, including Make Handout at `POST /v1/handouts`, Create Chart at `POST /v1/charts`, Make Object at `POST /v1/models`, and Make Molecule at `POST /v1/molecules`. [docs/AGENT_DIRECTIONS.md](docs/AGENT_DIRECTIONS.md) is the composition guide: which tool to pick, the palettes, how type is spaced, and the recipes for a poster, a flyer, a chart, a handout, and an object. Give an agent that file with the token. The server accepts tool calls. It does not turn an English sentence into a drawing.

## Limits

Two sessions, 40 calls each, 90 seconds absolute, 30 seconds idle, 256 KB JSON, 8 MB PNG, 8 MB PDF, 30 requests a minute. The figure renderer is `vectorcraft-cli mcp --headless`. A handout is one `typst compile` of a server-owned template. A chart is one `typst compile` of a second template. An object is one `openscad` render of a script, returned as a binary STL of at most 200,000 triangles. Each runs in a private scratch directory with a minimal environment. The token is not passed to those processes. Typst is pinned to 0.15.1 with MiTeX 0.2.7, Whalogen 0.3.0, CeTZ 0.5.2, CeTZ Plot 0.1.4, and the packages those load (xarrow 0.3.1 and oxifmt 1.0.0) vendored beside the binary, system fonts ignored, and package lookup limited to that vendor tree. OpenSCAD is a pinned official macOS build. Callers cannot choose a binary, a shell command, or a file path.
