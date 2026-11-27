# Plan

This is the working plan for building Forgeline. It is written so a fresh Claude Code
session (cloud or local) can pick it up with no other context. Update it as steps finish.

## Start here

Open this repository in Claude Code and paste:

> Read CLAUDE.md and docs/PLAN.md. Do the first item under "Next steps" that does not need a
> decision from me. Work on a branch, in small commits, run `make ci` before each push, open a
> pull request and stop so I can review. Update docs/PLAN.md and docs/ROADMAP.md as part of the
> change. Tell me what you verified and what you did not.

## Where things stand (updated 2026-11-27)

`main` is at pull request #243. [ROADMAP.md](ROADMAP.md) has the status of each milestone and feature, [status.md](status.md)
says what has been checked on real services, and each design note in [design/](design/) ends with what was and was not
checked. This page holds only where to start and what to do next.

**Built**
- Pipelines (M1, M2) and the web console (M2c), with seven spaces, single sign-on, approval rules and audit export.
- M3 slices 1 to 4: the network permission, secrets scoped to steps, a trust level per run, and audit of secret grants.
  Slice 5 is half built: every step drops all Linux capabilities.
- Cloud on AWS: blueprints on OpenTofu with approval, templates, policy packs, removal, native image builds.
- Agents: workflows, teams, schedules, missions, six model providers, MCP servers, export, traces, evals, runbooks with
  alarm intake, and the assistant as a sandboxed agent.

**Last tested as a user:** every guide on the maintainer's UAT instance on 2026-11-27, with the bugs found fixed in #243
([summary](testing/2026-11-27-uat-summary.md)). The full test ran before those fixes; after them only the console smoke
script was run.

**Layout of the code** (what to read first)
- `internal/pipeline`, `internal/scheduler`, `internal/executor`, `internal/cache`: the engine.
- `internal/server`: the API. Console files are `console_*.go`; `github.go` is the webhook; `runner.go` executes runs;
  `ui/` is the embedded front end (`ui/lib`, `ui/pages`, generated `ui/app.css`).
- `internal/docstore` (memory, file or Postgres documents), `internal/vault`, `internal/authn`, `internal/secrets`,
  `internal/redact`: console storage, crypto and masking.
- `web/tailwind.css` is the stylesheet source; `scripts/tailwind.sh` fetches the pinned CLI.
- `docs/design/`: one note per decision. 005 (threat model) is the one to read before touching trust.

## Decisions already made

| Decision | Choice | Where |
| --- | --- | --- |
| Name, license, visibility | Forgeline, Apache-2.0, public | README, LICENSE |
| Spec shape | Steps with `needs` form a DAG. No fixed stages | `internal/pipeline` |
| Execution engine | Own minimal executor behind an interface. Dagger later as an optional step type | `docs/design/001-execution-engine.md` |
| First container runtime | Call the `docker` CLI through `os/exec` behind the interface, no Docker SDK yet | this plan |
| YAML library | `sigs.k8s.io/yaml` so the existing `json` tags drive YAML too | this plan |

Change any of these by writing a short design note first.

## M1 steps

M1 exit criterion: `forgeline run pipeline.yaml` runs a sample pipeline on a laptop and skips
steps whose inputs have not changed.

### Step 2: YAML loader (done)

- Add `sigs.k8s.io/yaml`. Check its license and note it in the commit message.
- In `internal/pipeline`, add `Load(r io.Reader) (*Pipeline, error)` and
  `LoadFile(path string) (*Pipeline, error)`. Reject unknown fields. Call `Validate` and return
  its `*ValidationError` unchanged.
- Add `testdata/` pipelines: one valid, and invalid ones covering syntax errors, an unknown
  field, and several validation problems at once.
- Acceptance: YAML syntax errors say where they are. Validation errors keep field paths.
  Tests cover valid files, each invalid case, and an empty file.
- Known limit: converting YAML to JSON loses line numbers for validation errors. Decision:
  acceptable for now. Syntax errors keep line numbers and validation errors keep field paths.
  Revisit if users find paths hard to map back to the file.

### Step 3: DAG planner (done)

- Add `Plan(p *Pipeline) (*Graph, error)` that builds the dependency graph.
- Detect cycles and name the path in the error, for example `cycle: a -> b -> c -> a`.
- Produce layers: each layer holds steps whose dependencies are all in earlier layers, so a
  scheduler can run a layer in parallel. Order inside a layer is deterministic (declaration order).
- Acceptance: tests for a linear chain, a diamond, independent steps, a cycle, a self-cycle
  caught earlier by `Validate`, and a forward reference.

### Step 4: executor and scheduler (done; verified on real Docker)

- Define in `internal/exec` (or similar):
  `type Executor interface { Run(ctx context.Context, s StepRun, logs io.Writer) (Result, error) }`.
  `StepRun` carries image, command or script, env, workdir, timeout, and the workspace path.
- `DockerExecutor`: `docker run --rm` through `os/exec`. Mount the workspace at `/workspace`
  and use it as the default workdir. Apply the step timeout through the context. Return the exit code.
- `FakeExecutor` for tests. The scheduler must be fully testable without Docker.
- Scheduler: run the graph layer by layer with bounded parallelism. When a step fails, skip every
  step that needs it, run unrelated steps to completion, and report the full outcome.
- Acceptance: scheduler tests use the fake executor for success, failure, skipped dependents,
  timeout, cancellation, and parallelism limits. One test marked to skip when Docker is absent
  runs a real container.

### Step 5: design note 002, spec extensions (done)

Do this before step 6, because caching needs declared inputs and the trust model needs
permission fields in the spec.

- Write `docs/design/002-spec-extensions.md` covering:
  - `inputs`: file globs that make up a step's cache key.
  - `outputs` or artifacts, only if needed for the sample pipeline.
  - Reserved `permissions` fields for M3: secrets the step may read, network access,
    and whether it may receive untrusted input. Say clearly what is reserved and not enforced yet.
  - Cache key definition: image reference, command or script, env, workdir, and a hash of the input files.
- Add only the fields step 6 needs to the types, with schema regeneration and validation tests.
- Acceptance: the note states what is implemented now versus reserved.

### Step 6: `forgeline run` with caching (done; verified on real Docker)

- `forgeline run <file>` loads, validates, plans, and runs. Print one status line per step
  (ran, cached, failed, skipped). Exit non-zero on any failure.
- Cache: a local directory (for example `.forgeline/cache`) keyed as defined in note 002. A step
  whose key matches a previous success is reported as cached and not run again.
- Add `examples/hello/pipeline.yaml` with at least three steps, one of them dependent on two others.
- Acceptance: running the example twice shows the second run served from cache. Changing an
  input file re-runs only the affected steps. Document the example in the README.

When step 6 is done, mark M1 done in `docs/ROADMAP.md`.

## M2 steps (drafted, not started)

M2 exit criterion (from the roadmap): a single `forgeline server` binary backed by Postgres
accepts runs over a REST API, keeps run history, streams live logs, runs a pipeline for a
GitHub push or pull request event and reports commit status, and shows a DAG run view in a
browser.

Before step 1 starts, M1 should be proven once on a machine with Docker (see step 6 above).
Each step below ends with a green `make ci`; steps that touch trust or interfaces start with a
design note.

### Step 7: design note 003, control plane (done)

- Write `docs/design/003-control-plane.md` covering: the run and step state machines
  (queued, running, succeeded, failed, canceled; steps also cached and skipped), the data
  model, the REST resources, how logs are stored and streamed, and how a runner is attached.
- Decide and record: Postgres driver (candidate `pgx`, MIT), migration approach (embedded SQL
  files applied at start, no framework), HTTP stack (standard library `net/http`), auth for M2
  (a static bearer token from config; real identity is later).
- State what M2 does not do: no multi-tenant isolation, no sandbox beyond container defaults,
  no secrets management. Those are M3.
- Acceptance: the note names every table and endpoint step 8 to 12 will add.
- Done: `docs/design/003-control-plane.md`. Still open there: upload format and size limits,
  retention. The GitHub credential for M2 is a fine-grained token; a GitHub App comes later.

### Step 8: run store (done)

- Add `internal/store` with a `Store` interface for runs, step results and log chunks, plus an
  in-memory implementation used by tests and a Postgres implementation.
- Migrations are embedded SQL applied in order and recorded in a schema table.
- Acceptance: one shared test suite runs against both implementations. The Postgres run is
  gated on an environment variable so `make ci` passes without a database; CI sets it up.
- Done: `internal/store` (Memory, Postgres, embedded migration `0001_init.sql`). The shared suite
  passed against a real local Postgres 16 with `-race`; CI is configured to run it against
  Postgres 18 but that CI run has not been seen yet. Postgres tests use
  `FORGELINE_TEST_DATABASE_URL`.

### Step 9: server, REST API and embedded runner (done; verified on real Docker)

- `forgeline server --listen :8080 --database-url ...` serves: submit a run (pipeline YAML
  plus a source directory or checkout reference), list runs, get a run with step states, get a
  step's logs, cancel a run.
- The server executes queued runs with the existing scheduler and cache, in-process, with
  bounded concurrency. Separate runner processes are out of scope until a need appears.
- Every state change is written to the store; a server restart marks interrupted runs failed
  with a clear reason instead of leaving them running.
- Acceptance: handler tests with the fake executor cover submit, status transitions, failure,
  cancel, auth rejection, and restart recovery. `forgeline submit <file> --server URL` works.
- Done: `internal/server`, `internal/client`, `forgeline server` and `forgeline submit`. Tests
  cover all of the above plus hostile source archives and oversized uploads. The built binary
  was also exercised against a real local Postgres 16 (auth, persistence, failure reporting).
  Not run: a real Docker-backed run through the server (no Docker daemon in the dev sandbox).

### Step 10: live logs (done)

- Stream a step's logs with Server-Sent Events, replaying stored chunks first so a late
  client sees the full log.
- Acceptance: a test connects mid-run and receives the earlier lines then new ones in order;
  a finished run's log is served in full.
- Done: SSE in `internal/server/stream.go`, `client.StreamLogs`, and `forgeline submit --wait`
  now follows logs live. Tests cover replay then follow, multi-line chunks, resume with
  `Last-Event-ID`, steps that never ran, errors, and client disconnect. Verified with a fake
  executor only; not seen against real Docker output.

### Step 11: GitHub webhook trigger and commit status (done; not yet seen against real GitHub deliveries)

- Verify the webhook signature (HMAC SHA-256) before reading the payload, in constant time.
- On push and pull request events, fetch the repository at the commit, read the pipeline file,
  run it, and post commit status (pending, success, failure) through the GitHub API.
- Trust limit, stated in the code and docs: until M3, only repositories and branches on an
  allow list in the server config may trigger runs, and pull requests from forks are ignored.
  Running untrusted pull request code without the M3 model would contradict the project's own
  principles.
- Acceptance: tests cover a valid and an invalid signature, an event from a repository not on
  the allow list, a fork pull request being ignored, and status transitions against a fake
  GitHub API. Which GitHub credential type to use (GitHub App or fine-grained token) is decided
  in note 003.
- Done: `internal/server/github.go`, `forgeline server --github-allow`, decisions in note 003.
  Tests cover every acceptance case above plus redelivery, duplicates, the download limit,
  canceling a queued run, and restart recovery; two mutations (signature check bypassed, fork
  PRs allowed) were confirmed to fail the tests. Run by hand through the built binary with
  real Docker and Postgres against a local stand-in GitHub API, and the archive extractor was
  run on a real GitHub tarball of this repository. **Not done:** a real webhook delivery from
  GitHub and a real commit status, which need a public URL and a token and post to a
  repository, so they wait for the maintainer.

### Step 12: web UI with DAG run view (done; built before step 11 because the maintainer wants a web view to try)

- Static HTML and JavaScript embedded with `go:embed`, no build toolchain: a run list, a run
  page that draws the step DAG coloured by status, and a live log pane.
- Accessibility basics: status is never colour only, keyboard navigable.
- Acceptance: a test serves the UI; a manual check is recorded in the commit message, with a
  screenshot if a browser is available.
- Done: `internal/server/ui/`. Go tests cover serving, security headers and no inline script or
  style. A scripted Chromium (Playwright) check against a temporary dev server with a fake
  executor passed: sign-in and wrong token, run list, DAG with 6 steps and 7 edges, failed and
  skipped marking, live log, a hostile `<img onerror>` in log output rendered as plain text
  with no element created and no dialog, keyboard step selection, cancel, sign-out, no CSP
  violations, and no horizontal scroll at phone width. That browser script was not committed
  (it needs Node and Playwright); the dev server used was deleted. Not checked: dark mode
  visually, screen readers, browsers other than Chromium, real Docker output.

### Step 13: hosted demo on AWS (done; running now, see deploy/aws/README.md)

- Needs a decision from the maintainer first: account, region, budget, and public or private.
- Smallest viable shape: one container image of the server, a small managed Postgres, TLS, and
  an auth token. Infrastructure as code in the repository; no credentials in it.
- Acceptance: a documented, repeatable deploy and teardown, with the monthly cost estimated
  from current AWS pricing and checked against the budget before anything is created.
- Done: `deploy/aws/forgeline-demo.yaml` and `deploy/aws/README.md`. Decisions from the
  maintainer: ap-south-1, about $15/month, reachable only through SSM port forwarding. Price
  check (AWS Pricing API): t4g.small $0.0112/h, gp3 $0.0912/GB-month, public IPv4 $0.005/h,
  about $13.7/month. Stack `forgeline-demo` in the maintainer's AWS account created and verified
  (service active, health ok, real runs). The repeatable teardown is documented but was not
  exercised yet, so the first real delete is the test of it. The browser UI has not been
  checked through the tunnel on this deployment.

When step 13 is done, mark M2 done in `docs/ROADMAP.md`. (Steps 7 to 13 are now all built; the
roadmap exit criterion also asks for a real GitHub round trip, listed under step 11 above.)

## M2b: onboarding and connectors (designed, not started)

Design: [docs/design/004-onboarding-and-connectors.md](design/004-onboarding-and-connectors.md).
Steps O1 to O6 are listed there with how each is checked. It is blocked on three maintainer
decisions at the end of that note (GitHub sign-in, how AWS changes are applied, which CI/CD path
comes first). The GitHub webhook (step 11 above) is a prerequisite for O4 and for CI path 1.

## M3: trust and sandbox model (in progress)

Design: [docs/design/005-threat-model.md](design/005-threat-model.md), which lists the threats, their status and the five
slices.

- Slice 1, hardening and the network permission: **done.** `permissions.network: none|egress`, and `no-new-privileges` on
  every step. Tests run real containers.
- Slice 2, secrets scoped to steps: **done.** `permissions.secrets: [NAME]`, passed in a private env file, masked in
  logs, never cached. Masking does not cover transformed values (base64, split output).
- Slice 3, trust of the run: **done** (2026-11-25). Pull-request runs are untrusted and get no secrets; a push run gets
  them only where the repository's rule allows.
- Slice 4, audit log: **done** (2026-11-25). Secret grants, the secrets-plus-network combination and refusals are
  recorded, and the log pages back with a cursor.
- Slice 5: capability drop **done** (2026-11-26; `relaxed` by default, `strict` by setting, per-step grants audited, see
  [the measurements](testing/2026-11-26-step-capabilities.md)). **Memory, PID and CPU limits are not started.**

## Web console (design note 006: built)

Design: [docs/design/006-console.md](design/006-console.md). Storage and accounts, pipelines, a write-only vault, GitHub
and cloud pages, and changes that need a second administrator. No `innerHTML` anywhere (a test enforces it) and a strict
CSP. Still not checked: TLS behind a proxy and several server processes on one database.

## Next steps (in order)

Do these top to bottom. Items marked **needs a decision** wait for the maintainer; ask once, with a recommendation, and
do something else meanwhile.

1. **Finish M3: memory, PID and CPU limits for step containers** (slice 5). Defaults and how a step asks for more are a
   compatibility question, so measure typical steps first, as for capabilities. Possible follow-up: run steps as the
   workspace's owner, so `strict` needs no `DAC_OVERRIDE`.
2. **Second user test with an administrator test user**, on an instance that can be reset: administrator-only pages,
   approval rules, policy packs and runbook runs were not covered. **Needs a decision**: which instance.
3. **Verify against real services**, as the maintainer allows: a GitHub webhook delivery and commit status, single
   sign-on with a real provider, the other model providers, TLS behind a proxy, the demo stack teardown
   (`deploy/aws/README.md`, never run).
4. **Workflow canvas** follow-ups (note 044): run data per node, Team and sub-workflow nodes, re-run from a node.
5. **Agent Studio**, the rest of note 045: unsaved drafts and model comparison in the builder, publishing (agent keys, MCP
   tool, A2A card), code mode in a local sandbox.
6. **Assistant**, open items of notes 052 and 053: a host shell mode and a microVM.
7. **M4 security gates**: scanner steps in pipelines, SARIF findings, expiring waivers, SBOM and signing. Needs a design
   note first.
8. **Later:** audit forwarding to a SIEM with tamper evidence, a lead agent that hands out work, cost estimates, source
   checkout for agent runs, Codex and Copilot as pipeline steps, M2b onboarding re-scoped (note 004), the light helper
   model (note 038), Google Cloud and Azure (after the AWS work, note 040), M5 and M7.

## After M3 (outline only)

M4 security gates, M5 progressive delivery, M6 AI agents and M7 migration are in the roadmap. Plan each when the one
before it is done.
