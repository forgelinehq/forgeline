# Roadmap

Each milestone has exit criteria. A milestone is done only when its criteria are met and
demonstrated, not when the code exists. The order puts the trust model early because it is
the project's differentiator.

Status as of 2026-10-28: M0, M1 and M2 are built; M2c (the console) is built; M3 slices 1 and 2 are
built. The current state, what is verified only against stand-ins, and the ordered next steps are
in [PLAN.md](PLAN.md).

| # | Milestone | Exit criteria | Status |
| --- | --- | --- | --- |
| M0 | Foundation | Public repo, Apache-2.0 license, README, roadmap, architecture notes, Go skeleton that builds and tests locally | Done |
| M1 | Pipeline spec and local engine | Typed pipeline schema; YAML loader; DAG executor running container steps; `forgeline run` executes a sample pipeline on a laptop with caching of unchanged steps | Done |
| M2 | Control plane and UI | Single-binary server with Postgres; run history and live logs; GitHub webhook trigger with commit status; a DAG run view in the UI | Built. The GitHub round trip is verified only against a stand-in; see PLAN.md |
| M2b | Onboarding and connectors | Sign-in wizard: connect GitHub and AWS, discover ECS resources, reviewable plan, approved setup, connection tests, and a GitHub-to-AWS deploy workflow; see [design/004](design/004-onboarding-and-connectors.md) | Partly overtaken by the console (AWS read-only view, GitHub connection); to be re-scoped |
| M2c | Web console | Sign-in and roles; pipelines edited in the browser; secrets, GitHub and AWS connected from the UI; platform and cloud overview; cloud changes through proposal and approval; an audit log; see [design/006](design/006-console.md) | Built; see its list of what was not checked. Redesign for non-specialists in progress: [design/029](design/029-console-spaces-and-home.md) and [030](design/030-service-switches.md): Home, grouped sidebar, AWS service switches, visual system, assistant panel, agent screens, five spaces with tabs, the new Home and the Connections page built. Single sign-on (OIDC, roles from groups) built: [design/031](design/031-single-sign-on.md), not yet checked against a real provider. Approval rules built: [design/032](design/032-approval-rules.md). Audit export and retention built: [design/033](design/033-audit-export.md) |
| M3 | Trust and sandbox model | Per-step declared permissions; secrets scoped to steps; untrusted input isolated from secrets; audit log; a published threat model with tests that try to break it | In progress: slices 1 and 2 done; 3 to 5 open |
| M4 | Security gates | Scanner orchestration; findings normalized from SARIF; policy gates with expiring waivers; SBOM and image signing | In progress: Claude Code agent steps built (note 007); Codex, Copilot, triage and cost tracking open |
| M5 | Progressive delivery | One declaration for canary and blue-green on Kubernetes with metric-driven rollback | Not started |
| M6 | AI agents | Governed agent steps with scoped credentials and cost tracking; failure triage; fix suggestions as pull requests, never auto-applied | In progress: Claude Code agent steps built (note 007); Codex, Copilot, triage and cost tracking open |
| M7 | Migration and breadth | Importers for Jenkinsfile and GitHub Actions workflows; Bitbucket and GitLab triggers; additional cloud targets | Not started |

## Next: more blueprint blocks (note 039)

Status: **built (2026-11-08).** Databases (RDS MySQL or Postgres, with the password created and kept by AWS),
Redis, secrets whose values Forgeline never sees, one service calling another, autoscaling, and several paths behind
one front door. See [note 039](design/039-more-blueprint-blocks.md).

## Next: onboarding by conversation (note 038)

Status: **research done, needs a decision.** New-generation agents (OpenClaw's `openclaw onboard`, Hermes Agent's
`hermes setup`) get people working by asking questions, and can use a small model on the person's own machine.
First-run steps in the console and `forgeline onboard` are built. Next, revised by the maintainer: no llama.cpp or Ollama
and well under 500 MB, so a 25 MB embedding model running in the browser to match answers and find pages, then
FunctionGemma 270M fine-tuned on Forgeline's own steps only if an evaluation on the Pi shows it is reliable. See [note 038](design/038-conversational-onboarding.md).

## Open decisions

- Step execution engine. Proposed: write a minimal executor behind an interface and treat
  Dagger as an interoperable step type. See [design/001](design/001-execution-engine.md).
- Workflow state: a Postgres-backed executor to start, with the engine kept swappable.
- Policy language for gates (candidates: OPA, CEL). Not yet researched.
- Plugin model beyond container steps. WASM components are a candidate for small policy hooks.
- Verification of project name availability beyond a basic search. Not a trademark clearance.
