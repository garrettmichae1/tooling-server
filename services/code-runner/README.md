# Edsger remote IDE runner

Private Cloudflare Worker plus disposable, per-job microVMs. This is a new service:
the existing study-host image contains a tooling credential and must **never** run
submitted programs. No changes are needed to its existing public domain.

The iPhone calls the purchase-verification backend in `friendly-potato/services/agent-worker`.
That backend verifies the Apple transaction, code-sharing consent and shared
allowance before using its `CODE_EXECUTOR` private service binding. Both Workers
must be in the **same Cloudflare account**. Different domains are supported. This
runner has no public routes, workers.dev address, preview URLs or application secrets.

## Runtime catalog

| Image | Languages | Included libraries / execution |
| --- | --- | --- |
| Core | Python, JavaScript, TypeScript, Lua, Ruby, PHP, R, Perl, Bash, SQL, HTML/CSS | Python: NumPy, pandas, Matplotlib, SymPy, Pillow, requests. Node: lodash, Zod, React, React DOM, Vue; esbuild bundles TypeScript/JSX. SQL uses an in-memory SQLite database. |
| Systems | C, C++, Go, Rust | GCC C17/C++20, Go 1.27, Rust edition 2024; standard libraries, multi-file C/C++, offline Cargo projects. |
| JVM | Java, Kotlin | OpenJDK 21; checksum-verified Kotlin 2.2.21; standard libraries and multi-file builds. |
| .NET | C# | .NET 10 SDK, standard libraries and compatible source/project files. |
| Swift | Swift | Swift 6.2 for Linux, including Foundation. Apple SDKs, UIKit and SwiftUI are unavailable. |

React/Vue previews are static browser bundles, not persistent HTTP servers. HTML
inlines supplied CSS and scripts. The iPhone renders results in an ephemeral,
opaque-origin sandbox with denied outbound connections and no native bridge.
No package downloads occur during jobs. Additional libraries must be approved,
pinned in the image and tested; compatible vendored source can be submitted.
NuGet sources are cleared; Cargo and Go use offline resolution. Requests can be
imported, but remote HTTP APIs are unavailable. Other databases, native mobile
frameworks, GUI desktops and always-on backend services need a separate product design.

The systems image prebuilds Go's standard-library cache so cold jobs stay within
the compile/run deadline. Each job starts from that image seed on its own fresh
microVM disk. Writable compiler cache and program artifacts are destroyed with
the instance; no cache volume is shared between jobs.

## Boundary and limits

`src/contract.ts` defines `edsger-execution-v1`; the verified backend mirrors the
contract and validates it independently. `src/admission.ts` owns account-wide
limits; `src/index.ts` owns family routing and the one-use microVM lifecycle.
`runtime/runner.py` owns fixed compiler plans, unprivileged subprocesses, bounded
I/O and result preparation. None of these layers accept a shell command, installer,
URL, environment variable or price from the client. Add new adapters here and
update both contracts and the app's catalog in one coordinated review.

- Each job has a fresh backend-generated ID, a dedicated microVM and ephemeral disk.
- Submitted processes run as UID/GID 65532, with no supplementary groups, no privilege
  elevation and resource limits. The root HTTP controller never executes user source.
- Egress is disabled with `container.start({enableInternet:false})`; credentials,
  receipts and other users' files never enter the instance. The child environment is
  constructed explicitly and excludes inherited proxies and secrets.
- 30 seconds covers compilation plus execution; startup has 12 seconds. A durable
  alarm and inactivity limit enforce a 45-second instance lifetime. `destroy()`
  hard-terminates the whole instance after success, failure, cancellation or timeout.
- At most 64 text files / 256 KiB source; 8 KiB supplied input; 512 KiB transfer;
  64 KiB combined console output; 256 KiB preview. Source paths are relative,
  case-distinct and cannot collide with directories or `node_modules`.
- Global persisted launch limits: 8 concurrent jobs, 30 admissions/minute,
  500/day, 10,000/calendar month. Each image has a provisioning ceiling of 8;
  the admission actor caps the combined total. Abandoned leases count until expiry.
- No stdout/source logging or persisted source. Provider logs and Worker observability
  are disabled. Backend records contain only job IDs, language and accounting metadata.

The microVM is the security boundary; the subprocess adapter alone is **not** a
sandbox. Do not reuse instances, enable egress, introduce shared writable package
caches, add secrets to this service, or replace hard destruction with SIGTERM.

## Validation

From this directory:

```sh
npm ci --ignore-scripts
npm run typecheck
npm test
npm run test:runner
npm run build:check
docker build --platform linux/amd64 -f Dockerfile -t edsger-code-core:review .
python3 tests/image_smoke.py --profile core
```

Repeat image tests for `systems`, `jvm`, `dotnet` and `swift` with their Dockerfiles.
GitHub Actions runs the families separately. For a managed proxy environment,
Docker build supports optional CA mounts `proxy_ca` (npm) and `system_ca` (pip/curl);
the secrets are not copied into the final image. Official SDK base manifests are
pinned; npm uses its lockfile, pip requires hashes, and Kotlin verifies its archive.
OS packages receive build-time security updates; rebuilding requires the full matrix.
TypeScript uses esbuild transpilation; this release does not provide a language server.

## Rollout and rollback

Both production and sandbox have independent Workers, admission actors and containers.
`CODE_RUNNER_ENABLED` and the backend's `CODE_EXECUTION_ENABLED` ship **false**.
Deployment requires explicit approval of that exact deployment; this repository's
CI does not deploy. See the paired app PR's `Docs/REMOTE_IDE.md` for accounting,
membership limits and the full staged rollout.

After approval, deploy the sandbox runner **before** the sandbox backend so its
private service exists. Enable both sandbox gates only for a verified sandbox
subscription, then verify real container lifetime, denied egress, concurrency,
credit settlement and Stop. Repeat the approved process for production. Do not
add a public runner URL to work around an account or binding mismatch.

To stop new starts, turn off the backend gate and then the runner gate. DELETE
cancellation remains available with the gates off, and existing hard alarms must
remain deployed. Do not remove bindings or migrate/delete actors while jobs are live.
Turning off new starts does not stop all active jobs immediately: Stop and the
45-second alarm terminate them. An already-enabled paid Cloudflare account is
required; usage and deployment/image limits are subject to that account's billing.
