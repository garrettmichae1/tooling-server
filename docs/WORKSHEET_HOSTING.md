# Create Worksheet sandbox

The second iPhone tool is `make_worksheet`: written exercises, model answers,
worked reasoning, a printable question sheet, and a separate answer key. The
fixed Go template escapes every supplied string. It uses no model, subprocess,
external renderer, script, remote asset, or persistent educational content.
This is a specialized worksheet utility; the existing Typst handout renderer
and its richer typesetting remain separate.

## Contract and isolation

`POST https://tools-sandbox.edsger.app/v1/worksheets` accepts exactly:

```json
{"tool":"make_worksheet","arguments":{"title":"Algebra practice","instructions":"Show your working.","exercises":[{"prompt":"Solve 2x + 3 = 11.","answer":"x = 4","explanation":"Subtract 3, then divide by 2."}]}}
```

The edge, Go tool and iPhone independently enforce 1–30 exercises, a 256 KiB
encoded call, and UTF-8 byte limits: title 120, instructions 500, prompt 1000,
answer 1000, explanation 2000. Ambiguous JSON, unknown keys, invalid Unicode,
NUL, compressed bodies and oversized streamed content are rejected. The result
has `filename: worksheet.html`, two HTML strings (`html`, `answer_key_html`),
`offline: true`, `network_requests: false`, `preview_requires_scripts: false`,
and `content_verified: false`. Each HTML file is capped at 512 KiB; the complete
response is capped at 1,100,000 bytes. The question file contains no answer key.
Model-supplied facts are not independently verified.

Free rendering is an explicit policy for this deterministic utility. It uses
neither Apple authentication nor the iPhone payment worker. It cannot access
completions, model keys, personal API keys, receipts or an allowance ledger.
BYOK generation still goes directly from the phone to its provider; Dijkstra
generation still uses its existing authenticated paid completions route.

Admission requires the independent `WORKSHEET_HOST_ENABLED` gate, configured
origin credential, IP limiter and both worksheet bindings. Missing controls
fail closed. Cloudflare supplies the client IP; four attempts per IP per minute
are permitted. A separate singleton `WorksheetAdmission` SQLite object admits
30 renders per minute and 500 per UTC day across all clients. Its only record
contains counts and time buckets. Invalid stored counters fail closed. Counts
survive object/worker restarts; failures/replays conservatively consume a slot.
There is no user-identity claim or paid-credit charge on this public route.

`WorksheetContainer`, fixed name `worksheet-sandbox-v1`, is distinct from
`StudyContainer` and `study-sandbox-v1`. Each class permits at most one lite
instance. The worksheet process receives only `FIGURE_TOKEN` and the fixed
`WORKSHEET_ONLY=true` selector. It rejects quiz calls; the original quiz process
rejects worksheets. Both use the pinned scratch/nonroot Go image and disabled
internet. Free traffic cannot occupy the quiz process, reset its activity
timer, or change the iPhone payment deployments. Neither container logs content.

The public health response reports `worksheet_enabled` only when all worksheet
controls are present. The phone checks health before fresh model generation.
Quota races after generation retain the exact prepared content on the phone so
rendering can be retried without a second model charge. Once committed, a
worksheet can finish attaching to chat locally after a crash or origin outage.

## Validate

```sh
go test -race ./internal/tools ./internal/studyhost ./internal/httpapi
cd services/study-host
npm ci --ignore-scripts --no-audit --no-fund
npm run typecheck
npm test
npm run check:runtime
docker build --platform linux/amd64 --provenance=false -t edsger-study-tool:review -f Dockerfile ../..
npm run check:container
```

Tests cover real rate bindings, real SQLite caps/restart/day rollover, fixed
container names, credential separation, cancellation/deadlines, actual Go
rendering at 1/3/5/30 exercises, original quiz regressions and clean shutdown.
`WORKSHEET_TEST_OUTPUT` optionally writes fixed synthetic Go/native fixtures
from `TestWorksheetOriginIsolationAndNativeFixtures`. CI never deploys.

## Exact deployment to review before approving

Deploy only after approval. The target is **edsger-study-tool-sandbox** at
**tools-sandbox.edsger.app**. It adds two SQLite classes under the additive
`v2-worksheet-isolation` migration and one separately limited worksheet
container class. Existing `v1-study-container`, origin secret and quiz object
remain in place. Do not delete objects or reset budgets. Both checked-in flags
default false; explicitly preserve the currently approved quiz flag when
deploying this new image and enabling the worksheet flag.

```sh
cd services/study-host
npx wrangler deploy --var STUDY_HOST_ENABLED:true --var WORKSHEET_HOST_ENABLED:true
```

Use the existing private Wrangler login without printing credentials. Record
current tooling and both iPhone worker version IDs before and after. Verify
health and one fixed free worksheet, then confirm original quiz behavior.
This action does **not** deploy `lilc-agent` or `lilc-agent-sandbox`. The app's
release worksheet gate remains false; Debug iPhone testing enables the
separate worksheet switch in Settings. A production release needs its own
approved origin/gates and privacy review.

To stop only new worksheets, deploy this same tooling config with
`STUDY_HOST_ENABLED:true` and `WORKSHEET_HOST_ENABLED:false`. Saved native
worksheets, answers and PDF sharing remain available offline. Keep the new
class migrations; removing a public route must never delete persisted state.
