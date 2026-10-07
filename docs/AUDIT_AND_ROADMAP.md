# Repository audit and expansion plan

## Starting point

The repository was a Go service for five output families: VectorCraft drawings/posters, Typst handouts, Typst charts, OpenSCAD objects, and curated molecular models. Existing strengths included loopback binding, bearer authentication, allowlisting, deadlines, private scratch directories, output limits, and unit/mock-renderer tests. There was no app code, model orchestration, billing, public gateway, or general tool discovery.

The main barrier for smaller models was a long prose contract, detailed rendering arguments, multi-call drawing sessions, and generic rejection errors. General arithmetic, data cleanup, study grading, and planning lacked dedicated tools.

## Changes

| Finding | Change |
| --- | --- |
| Rendering was the only capability | 32 utilities across eight categories |
| No function catalog | Authenticated schemas/examples, category filtering, per-tool discovery |
| Many endpoint shapes | Uniform call envelope plus direct per-tool routes |
| Long art instructions consumed context | Separate short quickstart |
| Little help correcting arguments | Utility errors identify code, field, and correction |
| Every binary required at startup | Explicit `FIGURE_TOOLS_ONLY=1`; default full mode preserved |
| Relative binaries broke in child scratch directories | Resolve executable paths before launching |
| Bad credentials/health could exhaust the rate limit | Authenticate before accounting; exclude health |
| Uncapped PDF/STL concurrency | Shared two-slot cap with retry headers |
| Parallel session startups could exceed the cap | Reserve startup capacity before launching |
| Full-file reads preceded size checks | Bounded regular-file reads for PNG/PDF/STL |
| Some unusable token/bind values were accepted | Token control/length checks and port bounds |
| Docs suggested putting credentials with the model | Credentials remain in the trusted caller |

Legacy endpoint names and successful response formats remain intact. Utilities add no subscription checks, provider keys, network fetches, or caller-code execution. All changes live in this repository.

## Accuracy and verification

Checks include known answers, schema/negative boundaries, unit/encoding round trips, exact rational arithmetic, Unicode/escaping, CSV multiline quoting, UTC calendar conversion/folding, WAV header/frequency/amplitude checks, UUID bits, every example through both API forms, authentication/capacity, concurrent calls, and fuzz seeds for every tool.

This is evidence for the stated contracts, not proof for all possible inputs or a promise of semantic grading, factual content, perfect dense SVG routing, or successful selection by every model. Float64 operations are approximate. Real legacy renderer output requires the pinned binaries and real integration tests; mock tests do not establish visual quality.

## Integration boundary

A free/local model needs an agent loop supplying relevant schemas, validating the proposed call, attaching credentials, invoking the service, and feeding the result back. Hosted use also needs connectivity, per-user gateway authentication/limits, TLS, and policy. Do not embed a shared server token in a distributed phone app.

No Edsger app, model worker, subscription policy, or other repository changed. Utilities are server-ready, not automatically offline on the phone. This service remains loopback-bound and is not a public multi-tenant gateway.

## Next priorities

The goal is a broad verified library. One reliable conversion engine already provides many operations; aliases alone would inflate the count without adding value.

| Priority | Capability | Required work/evidence |
| --- | --- | --- |
| 1 | Evaluate actual Edsger models | Tool selection, arguments, repair, task-success corpus |
| 1 | Public lookup: weather, encyclopedia, papers, books | Source-backed adapters, citations, caching, network bounds, live tests, outage/rate handling |
| 1 | Search caller-owned documents | Extraction limits, source/page/line citations, PDF/DOCX parser tests |
| 1 | Background artifact jobs | Durable storage, ownership, cancellation/idempotency, expiry/restart tests |
| 2 | Exact symbolic math | Constrained parser/engine, reference cases, process resource caps |
| 2 | Renderer-free rich SVG charts | Axis/scaling tests, multiple series, negative values, layout QA |
| 2 | Documents/spreadsheets | Templates, pagination/render QA, metadata/formula handling |
| 2 | Metronomes, note sequences, audio analysis | Reference samples, codec/header tests, duration/amplitude bounds |
| 2 | Scheduling/travel calculations | Explicit holiday/timezone/routing sources; export versus actual scheduling |
| 3 | Offline educational reference packs | Licensing/attribution, versioned updates, reference-data tests |
| 3 | Opt-in multi-language execution | OS/container isolation; no shared credentials; CPU/memory/disk/network quotas |

Shared deployment still requires real renderer process isolation, user ownership/rate accounting, monitoring, and platform-specific work. Private scratch directories and minimal environments do not enforce OS-level memory, CPU, disk, or network quotas. Standard-library utilities avoid renderers entirely.
