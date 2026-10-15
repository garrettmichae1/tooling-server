# Create Word Search: server and native caller contract

`make_word_search` converts a supplied word list into a deterministic letter
grid, structured solution coordinates, a printable puzzle, and a separate
printable answer key. The server uses no model, subprocess, remote renderer,
script, external asset, or persistent puzzle storage. It verifies placements
and uniqueness; it does not verify word meanings or theme relevance.

This change prepares the server only. **No iPhone code is changed, no native
word-search UI is implemented, and no word-search deployment is performed.**
Sandbox and production gates default off. Live activation requires approval of
the exact target and deployment; prior quiz/worksheet approval is not approval
to activate this tool.

## Request

The future native caller sends `POST /v1/word-searches` to its environment:

| Environment | Host | Worker |
| --- | --- | --- |
| Sandbox | `https://tools-sandbox.edsger.app` | `edsger-study-tool-sandbox` |
| Production | `https://tools.edsger.app` | `edsger-study-tool` |

Required headers are `Content-Type: application/json` and
`X-Edsger-Word-Search-Consent: v1`. The consent header asserts that the caller
has obtained permission to send the generated title and words. It is not
authentication or proof of identity. Do not embed the private origin token in
the phone. This independently limited rendering route does not require or
forward an Apple receipt or model-provider key.

Fixed request, with a retained seed for replay:

```json
{"tool":"make_word_search","arguments":{"title":"Garden visitors","words":["BEE","ANT","MOTH"],"difficulty":"easy","size":8,"seed":42}}
```

Save that JSON as `word-search-request.json`. After an approved activation,
this fixed synthetic request can be sent without any model or payment call:

```sh
curl --fail-with-body --max-time 30 \
  -H 'Content-Type: application/json' \
  -H 'X-Edsger-Word-Search-Consent: v1' \
  --data-binary @word-search-request.json \
  https://tools-sandbox.edsger.app/v1/word-searches
```

The authenticated loopback catalog exposes the same utility at
`POST /v1/tools/call` and `POST /v1/tools/make_word_search`. The dedicated
internet-facing host accepts only its narrow route and tool, not the full
catalog or other study tools.

| Argument | Contract |
| --- | --- |
| `title` | Required nonblank string, at most 120 UTF-8 bytes; surrounding Unicode whitespace trimmed. |
| `words` | Required array of 1–30 strings, each at most 64 UTF-8 bytes before trimming. Input order is preserved. |
| `difficulty` | Required `easy`, `medium`, or `hard`. |
| `size` | Optional integer 8–24. Omit for deterministic automatic sizing; an explicit size is fixed. |
| `seed` | Optional integer 0–2147483647. Omit to derive it deterministically from the normalized request. |

Only these fields are accepted. The entire encoded request is at most 16 KiB.
Unknown or duplicate object keys, trailing JSON, invalid Unicode, wrong types,
NUL and compressed bodies are rejected. A word longer than an explicit grid
size is invalid. JSON numeric forms such as `8.0` or `1e1` are accepted when
their decoded binary64 value is integral and within bounds. Clients should
encode integer values; arbitrary-precision decimal input is not promised.
Canonical replay/hash numbers use ordinary base-10 integers.

Words support **English ASCII letters** and interior ASCII spaces, hyphens and
apostrophes. Surrounding ASCII spaces are trimmed; the retained spelling is
the `label`. Separators are removed and letters uppercased to form `letters`:
`" honey-bee "` becomes label `"honey-bee"`, letters `"HONEYBEE"`. Each normalized
word must have 3–18 letters. Digits, punctuation outside those separators,
accented/non-Latin letters, tabs and other whitespace are rejected. There is
no transliteration, silent splitting, truncation or dropping. Normalized
duplicates and reverse pairs such as `CAT`/`TAC` are rejected; palindromes are
accepted.

## Model boundary

The selected model generates a bounded **word list only**. The trusted caller
sets the user's size/difficulty and validates the list before HTTP. The model
must not produce a grid, placements, HTML or executable code. A small future
model instruction can be:

> Return only JSON of the form `{"words":["BEE","ANT","MOTH"]}`. Produce exactly
> 3 distinct English words about garden insects. Use only ASCII letters, with
> spaces, hyphens or apostrophes inside names. After removing those separators,
> each word must have 3–18 letters. Do not include reversed duplicates. Do not
> generate the puzzle grid or code.

This prompt is an integration example, not evidence of reliable output from
any particular model. Model generation remains outside this server; existing
BYOK or Dijkstra access, consent and billing still apply to generation. A
future caller should preflight `/health` and require `word_search_enabled:
true` from a fresh response before spending model work, then save the exact validated request for
rendering retries. A quota race can still occur after preflight.

## Success response and native rendering

The envelope is exactly `{"ok":true,"tool":"make_word_search","result":...}`.
The result contains these fields. [Actual generated fixtures](../services/study-host/tests/fixtures/word-search)
contain `{input,response}` pairs at 1/3/5/30 words in all three difficulties,
plus palindrome, substring and normalization cases. The response grids are
produced by the Go engine, not hand-written examples.

| Field | Meaning |
| --- | --- |
| `schema_version` | Integer `1`. |
| `generator_version` | `"wordsearch-v1"`; pins generation and replay behavior. |
| `puzzle_id` | Stable 64-character lowercase SHA-256 hex identifier. |
| `title`, `difficulty`, `size`, `seed` | Trimmed title, requested difficulty, actual grid size, resolved seed. |
| `directions` | Ordered allowed direction names for this difficulty. |
| `grid` | `size` strings, each exactly `size` uppercase ASCII letters. Row 0 is the top row. |
| `words` | Input-order objects `{id,label,letters}`; `id` is the zero-based input index. |
| `placements` | One input-order object per word: `{word_id,direction,start,end,cells}`. |
| `filename`, `media_type` | `"word-search.html"`, `"text/html; charset=utf-8"`. |
| `html` | Escaped, script-free printable puzzle sheet, without highlighted solutions or placement data. |
| `answer_key_html` | Separate escaped, script-free printable sheet highlighting solutions. |
| `offline`, `network_requests`, `preview_requires_scripts` | `true`, `false`, `false`. Saved artifacts require no network. |
| `puzzle_verified`, `content_verified` | `true`, `false`: mechanical placement/uniqueness checked; meanings/theme not independently checked. |

Coordinates are objects `{row,column}`, both zero-based integers within the
grid. Column 0 is the left column. `cells` is ordered from `start` to `end` and
has one cell per normalized letter. Direction deltas use screen coordinates:

| Direction | Row delta | Column delta |
| --- | --- | --- |
| `E` | 0 | +1 |
| `S` | +1 | 0 |
| `SE` | +1 | +1 |
| `NE` | -1 | +1 |
| `W` | 0 | -1 |
| `N` | -1 | 0 |
| `NW` | -1 | -1 |
| `SW` | +1 | -1 |

Allowed ordered arrays are `easy: [E,S]`, `medium: [E,S,SE,NE]`, and
`hard: [E,S,SE,NE,W,N,NW,SW]`. Every target has exactly one physical contiguous
straight-line occurrence when searching **all eight directions**, including
directions unavailable for intentional placements at easier difficulties.
For palindromes, forward/reverse matches occupying the same physical cells
count once. Substring targets may share cells if uniqueness is maintained.
Every supplied target appears or the entire generation fails.

The complete response is capped at 256 KiB and each HTML string at 96 KiB.
The future iPhone caller should bound the streamed response before decoding,
reject unsupported versions/unknown fields, and independently check:

1. Envelope, flags, types, byte bounds, grid dimensions and uppercase alphabet.
2. Exact input-order words and IDs, normalized spelling, title/difficulty and
   any explicitly requested size/seed. Omitted size must resolve within 8–24;
   omitted seed must match its deterministic derivation.
3. One placement per word, allowed direction, in-bounds contiguous cells,
   matching endpoints, length and letters, and exactly one physical occurrence
   across all eight directions. A `puzzle_verified` flag alone is insufficient.
4. `puzzle_id` using the versioned hash recipe below.

A native interactive grid should use `grid`/`placements`, with IDs for found
words and local client-owned progress keyed by puzzle ID and generator
version. A future client can accept a selection in either traversal order
when it identifies the recorded physical path. Do not send player selections
or progress to this renderer. Keep a saved immutable puzzle so offline resume
does not depend on regenerating it. The primary native contract is structured
JSON, not executable HTML. The HTML exports are printable sheets; they do not
implement touch selection, saved progress, hints or a native UI. Keep scripts
and remote resources disabled when preparing those sheets for printing. The
client must save/share the two sheets separately.

**Placements are deliberately present in the response.** Answer-key secrecy
is not provided: this is a practice activity, not secure assessment. Users
should be able to reveal answers, while ordinary puzzle printing should use
only `html` and answer-key printing only `answer_key_html`.

## Determinism and hashes

For both hashes, encode each listed string as UTF-8, prefix it with its byte
length as a four-byte unsigned big-endian integer, concatenate in order, then
SHA-256 the bytes. Numeric strings are canonical base-10 without leading
zeros. Labels preserve supplied trimmed spelling; title uses Go `TrimSpace`.

Missing-seed input strings are:

```text
wordsearch-v1-seed, title, difficulty, requested-size (0 if omitted), word-count,
label-0, letters-0, label-1, letters-1, ...
```

Take the first four digest bytes as unsigned big-endian and mask with
`0x7fffffff`. For `puzzle_id`, the strings are:

```text
wordsearch-v1, title, difficulty, actual-size, resolved-seed, word-count,
label-0, letters-0, label-1, letters-1, ...,
grid-row-0, grid-row-1, ...,
direction-0, start-row-0, start-column-0, end-row-0, end-column-0,
direction-1, start-row-1, start-column-1, end-row-1, end-column-1, ...
```

The hash identifies immutable content; it is not an authenticated signature
or proof of model correctness. Retain the request and generator version.
Identical canonical input replays the same result; a new generator version
must not overwrite old saved artifacts. Automatic-size requests should be
retried as the original request, rather than substituting a returned size.

Generation uses [fixed work/restart ceilings](../internal/tools/word_search.go)
and returns `generation_failed`
when it cannot satisfy the exact contract. It never returns a partially filled
puzzle or silently omits words. Replaying the same seed/request repeats that
outcome. To attempt a different layout, retain the validated word list and
change only the seed or increase size within 8–24; there is no need for another
model call. Some word sets are impossible under the uniqueness rule, such as
`AAA` together with `AAAA`; revise those words instead of retrying indefinitely.
Invalid content must be corrected rather than retried unchanged.

The public route returns fixed codes without reflecting supplied words or
upstream error text:

| Status | Code | Caller action |
| --- | --- | --- |
| 400 | `invalid_request`, `https_required` | Correct invalid input or use HTTPS. |
| 403 | `consent_required` | Obtain sharing consent before sending content. |
| 404 / 405 | `not_found` / `method_not_allowed` | Use the documented POST route. |
| 409 | `request_cancelled` | Preserve the request; retry only if the user still wants creation. |
| 413 / 415 | `payload_too_large` / `unsupported_media_type` | Respect bounds and send uncompressed JSON. |
| 422 | `generation_failed` | Try another seed or larger size, or revise an incompatible word list. No partial result or automatic retry header. |
| 429 | `rate_limited` | Honor `Retry-After: 60`; retain prepared content. |
| 503 | `service_unavailable` | Retain prepared content and retry later; honor `Retry-After` when provided. |

The generic authenticated utility API additionally offers field-specific
argument corrections. Never display raw origin/provider errors or restart
model generation merely because rendering failed.

## Hosting, privacy and isolation

The independent `WORD_SEARCH_HOST_ENABLED` flag defaults false. Missing
`FIGURE_TOKEN`, `WORD_SEARCH_IP_LIMIT`, `WORD_SEARCH_ADMISSION` or
`WORD_SEARCH_CONTAINER` fails closed, and health advertises
`word_search_enabled` only when every required control exists.

Cloudflare-supplied IPs permit four attempts per 60 seconds using separate
namespaces 1005 (sandbox) and 1006 (production). A singleton
`WordSearchAdmission`, object name `word-search-admission-v1`, persists a
global 30-render/minute and 500-render/UTC-day budget in SQLite. Its data is
only time buckets and counters; no puzzle, word list, identity or player
progress is stored. Every admitted attempt, including failed generation and
replay, consumes budget; invalid requests are rejected before global admission.
Counters survive worker/object restart and invalid stored state fails closed.
These are abuse/resource limits, not paid-user entitlement checks.

`WordSearchContainer`, fixed object `word-search-v1`, has at most one `lite`
instance per environment. The scratch/nonroot Go container disables internet
and receives only the private origin token plus `WORD_SEARCH_ONLY=true`.
The word-search process rejects quiz/worksheet calls; existing study processes
reject word-search calls. Enabling multiple `*_ONLY` selectors fails closed
at startup. The edge strictly verifies grid/placements, input binding and
hash before returning success; untrusted upstream flags cannot bypass checks.
Sandbox and production have independent credentials, objects and quotas.

Consent covers the title and generated word list crossing the network. The
route neither receives nor forwards raw chat history, source documents,
personal model keys, Apple receipts or saved player answers. Do not log those
items, request bodies, puzzle content or private origin credentials. This
utility does not access payment workers, completions or allowance ledgers.

## Validation and future deployment

Use the repository's pinned Go and Node versions:

```sh
go test -race ./internal/tools ./internal/studyhost ./internal/httpapi
go vet ./...
go run scripts/word-search-fixtures.go -check
python3 scripts/validate-word-search-fixtures.py --self-test
cd services/study-host
npm ci --ignore-scripts --no-audit --no-fund
npm run typecheck
npm test
npm run check:runtime
docker build --platform linux/amd64 --provenance=false -t edsger-study-tool:review -f Dockerfile ../..
npm run check:container
```

Validation includes deterministic replay, independent uniqueness checks,
bounded failures, escaping, catalog/HTTP calls, narrow-origin cross-tool
rejection, strict edge response verification, actual workerd/SQLite quota
persistence and day rollover, and actual nonroot/read-only container behavior.
Native app integration and real model word-list evaluation are future work;
server tests do not establish either.

**Deployment is not performed by this change.** Before requesting activation,
record current tooling source/version and live quiz/worksheet gate values for
the selected environment. Review the exact target, additive
`v3-word-search-isolation` migration (`WordSearchContainer`,
`WordSearchAdmission`), one additional lite container, and admission budget.
Keep the existing v1/v2 migrations, credentials and objects. Do not deploy
the iPhone app, either payment Worker, or the private code runner.

If the selected environment's existing quiz and worksheet gates are both
confirmed `true`, the future **sandbox-only** command to review is:

```sh
cd services/study-host
npx wrangler deploy --var STUDY_HOST_ENABLED:true --var WORKSHEET_HOST_ENABLED:true --var WORD_SEARCH_HOST_ENABLED:true
```

Run it only after approval of that exact deployment. Preserve observed
existing gate values rather than assuming they are enabled. Production needs
separate approval and its own `--env production` deployment; approving sandbox
does not approve production. Verify health, fixed requests at 1/3/5/30 words,
original quiz/worksheet routes, quota/error behavior, and unchanged payment
Worker version/ledger IDs. Credentials remain in private Wrangler storage.

To stop new word searches, redeploy the same selected environment with
`WORD_SEARCH_HOST_ENABLED:false`, preserving its current quiz/worksheet flags.
Keep additive migrations and persisted budget state. Existing client-saved
puzzles remain playable/printable offline; shutting the creation route does
not erase them.
