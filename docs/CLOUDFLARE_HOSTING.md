# Isolated study-tool hosting

The first hosted pilot is a separate Cloudflare Worker and Go container at
`https://tools-sandbox.edsger.app`. The `edsger.app` website and both live iPhone
API Workers retain their own addresses, deployments, ledgers, and credentials.
This repository does not deploy or enable the iPhone payment gateway.

The resources are `edsger-study-tool-sandbox`, a new `StudyContainer` SQLite
Durable Object namespace, and at most one `lite` container. All authenticated
requests use the fixed object name `study-sandbox-v1`; input cannot select another
instance, host, tool, file, or executable. Containers requires Workers Paid and
consumes that account's infrastructure allowance; Cloudflare meters excess usage.
There is no paid model call. See [Cloudflare plans](https://developers.cloudflare.com/workers/platform/pricing/)
and [Containers pricing](https://developers.cloudflare.com/containers/pricing/).

## Execution and security

`cmd/studycontainer` is an explicit hosted entry point. It listens on port 8080
inside the private container network and invokes only `make_study_app` through
the existing pure-Go utility implementation. The original `cmd/figureserver`,
its numeric-loopback bind validation, all local tools, and all legacy renderers
remain unchanged. No rendering binaries or paid-model dependencies are installed.

The image uses an official Go 1.27.0 build image pinned by registry digest, then
a `scratch` runtime containing one static executable, with user `65532:65532`.
The Docker context is an allowlist of the required source and embedded assets.
The Cloudflare class disables internet access and stops the container after ten
minutes without activity. The container has no educational-content persistence.
Its Durable Object stores SDK lifecycle metadata, not questions, answers or HTML.
Worker and container observability are disabled; process errors use fixed codes.

The public edge accepts only HTTPS and, when Cloudflare provides the TLS version,
TLS 1.2 or 1.3. It exposes:

- `GET /health`: **edge liveness only**, with the host's enabled/configured state.
  Public health checks do not start a container or prove the Go process is ready.
- `POST /v1/tools/call`: server-only bearer authentication and exactly
  `{ "tool": "make_study_app", "arguments": { ... } }`.

The edge verifies the credential before reading input or looking up a container.
There is no public catalog, direct-tool route, legacy renderer, upgrade, query
credential, or user-selected upstream. The edge shares the app gateway's strict
first-tool contract: 262,144 UTF-8 bytes, one to thirty questions, bounded strings,
distinct choices, correct indices, and no unknown fields, duplicate JSON keys,
NUL, invalid UTF-8, unpaired surrogates or trailing JSON. It forwards only the
canonical tool payload and origin credential; Apple receipts and client headers
never go to the container. Go independently restricts the path, tool, body size,
question count and utility schema. Its process-wide limit is 30 requests per
minute with one render at a time; health and wrong credentials consume no slots.

Responses are bounded to 1,100,000 bytes and must match the existing HTML-export
contract, including offline/no-network and unverified-content flags. Errors expose
only fixed codes. All responses are JSON with `no-store` and `nosniff`; no HTML is
served as an executable page by the origin. The iPhone renders native quiz UI and
can separately share the offline HTML file. Content correctness remains the
local model's responsibility; tool execution does not verify its facts.

Cold-start plus response work has a 14-second edge deadline, inside the app
gateway's 15-second deadline. An observed cancellation returns promptly, but
cross-Worker disconnect propagation is not guaranteed. A slow cold start returns
503 and `Retry-After: 5`. Recover using the app's saved canonical payload and
stable request ID; do not regenerate questions. Local-model quality and physical
iPhone/iPad sandbox acceptance remain separate from hosting verification.

## Verify before deploying

From the repository root, run `go vet ./...` and `go test -race ./...`.
Then:

```sh
cd services/study-host
npm ci --ignore-scripts --no-audit --no-fund
npm run typecheck
npm test
cd ../..
docker build --platform linux/amd64 --provenance=false \
  -t edsger-study-tool:review -f services/study-host/Dockerfile .
cd services/study-host
npm run check:container
```

The last command launches an isolated nonroot/read-only Docker container, tests
its actual artifact and rejection paths, bundles the complete Workers SDK, and
passes one/three/five/thirty-question quizzes plus deterministic replays from workerd
through a real SQLite Durable Object transport to the real Go image. It sends
only synthetic quiz fixtures and cleans up its own container. It requires a local
Linux Docker daemon at `/var/run/docker.sock`; no Cloudflare login is needed for
tests. GitHub Actions runs these checks without deployment credentials.

## Deploy this service only

1. Obtain approval for this exact separate sandbox deployment and confirm
   Workers Paid/Containers access. Record both existing iPhone API deployment
   versions and their health responses before changing the new service.
2. From `services/study-host`, run `npx wrangler login --device` and confirm the
   account with `npx wrangler whoami`. Credentials belong outside the repository.
3. Run `npx wrangler deploy`. The checked-in origin flag defaults to `false`, so
   the new service fails closed while its server credential is missing.
4. Generate a strong random credential privately and install it with
   `npx wrangler secret put FIGURE_TOKEN` using its hidden prompt/standard input.
   Never use a command argument, source file, log or app-bundled token. Keep it
   in private operator storage for the future separately approved gateway setup.
5. Run `npm run deploy:sandbox` to enable this authenticated origin explicitly.
   This command changes only `edsger-study-tool-sandbox` and the new custom domain;
   it does not deploy either `lilc-agent` Worker.
6. Verify public edge health, rejection of missing/wrong credentials and other
   tools, then privately POST fixed one/three/five/thirty-question fixtures and replay
   them identically. A valid authenticated render proves Go/container readiness.
7. Compare both existing iPhone API deployment versions and health responses
   against the baseline, and record the new Worker version/container image in
   the deployment handoff without credentials or educational content.

For a kill switch, redeploy **this host's config** with
`npx wrangler deploy --var STUDY_HOST_ENABLED:false`. Do not run commands from
the payment Worker directory. Its old instance sleeps after inactivity. To
rotate `FIGURE_TOKEN`, disable this origin, replace its secret, restart its
isolated container so Go receives the new environment, verify privately, and
then re-enable. A warm process retains its previous environment until restart.

The app gateway's future sandbox configuration is
`STUDY_TOOL_ORIGIN=https://tools-sandbox.edsger.app`, the same credential under
its separate `STUDY_TOOL_TOKEN` secret, and an immutable source revision under
`STUDY_TOOL_REVISION`. That is a separate rollout: retain the existing Apple
verification, consent, ledger, idempotency, rate limits and off-by-default app
switch. Hosting this origin does not authorize a payment-backend deployment,
production enablement or additional tools.


## Second tool: worksheets

The original quiz process remains quiz-only. The additive worksheet implementation has its own admission object, IP limiter, feature gate and container class; see [WORKSHEET_HOSTING.md](WORKSHEET_HOSTING.md) for its free policy, quotas, verification and exact deployment. Enabling it requires explicit approval and must preserve the quiz flag.
