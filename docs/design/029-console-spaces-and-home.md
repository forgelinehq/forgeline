# 029: Console spaces, Home, and the redesign plan

Status: **Slice 1 built (spaces and Home's Needs you list). The rest is planned.** Date: 2026-11-07.
Builds on notes 006 (console), 014 (missions), 017 (agents, environments), 022 (first redesign), 024
(agent surface), 025 (integrations hub) and 028 (assistant actions). Facts about the code are from
`origin/main` at `a5f6164`. A visual plan with mockups and a task board were reviewed with the
maintainer before this note; they are design aids and are not part of the repository.

## Question

Forgeline is meant to let people who are not infrastructure specialists run cloud, delivery and AI
work. The console is organised by system (Pipelines, Actions, Infrastructure, Guard, Agents,
Marketplace, Approvals and nine Settings pages), its first screen counts CI runs only, and nothing tells a
person what is waiting for them. A walk through the live console on 2026-11-07 showed two
high-severity Guard findings and changes waiting for approval, none of them visible on the first page.
How should the console be organised, and what should the first screen be?

## Decisions

| Decision | Choice |
| --- | --- |
| Spaces | The sidebar is grouped by job: **Home** (what needs me) and **Approvals** at the top, **Ship** (Pipelines, GitHub Actions), **Build** (Environments; the design canvas and blueprints later), **Team** (Agents, Marketplace; missions and budgets later), **Watch** (Infrastructure, Kubernetes, Guard). Settings stays behind the gear (note 025). Routes, role checks and addresses do not change |
| Home | The first page leads with **Needs you**: changes the person can approve, pipelines whose last run failed, open critical and high Guard findings, and connections that need attention, most urgent first. Tiles above it count the same things. The pipeline tables stay below |
| Who is asked to approve | Only an administrator, and only for changes someone else proposed. A person's own proposal is listed as waiting. This mirrors the server's rule; the list is a pointer, and the server still refuses a self-approval |
| Where the data comes from | Existing read endpoints, each only when the person's role may read it (`/v1/integrations` is for developers and up). A source that cannot be read is left out: the list says less, never something wrong. No new endpoint, so no new way to read anything |
| Not scanned is not safe | When Guard has no finished scan, the Security tile says "not scanned yet" instead of showing zero findings |
| Refresh | Pipeline figures every 4 seconds as before; approvals, findings and connections every 32 seconds |
| Logic outside the DOM | `ui/lib/inbox.js` and `ui/lib/nav.js` have no imports and no DOM, so tests run them under Node, like `settingsnav.js` |

## The plan after this slice

The full plan has six phases. Each starts with its own note where it changes trust or interfaces.

1. **Foundations:** design tokens, fonts served from the binary (the CSP stays `'self'`), a component kit, one SVG graph component grown from `ui/lib/dag.js` for pipelines, missions, the canvas and the map, a Simple mode that hides YAML and tool names, and a Playwright screenshot suite.
2. **Home and Connections:** the ask bar (search, commands and the assistant in one box); connections as a short list where each provider's **services** (for AWS: S3, ECS, ECR and so on) are switched on one at a time with a plain access level (Look only, Look and suggest, Change with approval) that maps onto the existing per-tool policy (note 026). A switched-off service is refused on the server.
3. **Build:** a typed Blueprint model (Go types first, schema generated), a design canvas beside the assistant, OpenTofu generation for AWS, and the plan drawn on the canvas as the preview a person approves.
4. **Team:** coding engines behind one interface (Claude Code built; Codex `exec` and Copilot CLI next), agents with roles and a manager, heartbeats, budgets with hard stops, and a missions board (note 014).
5. **Autonomy and recovery:** a snapshot before every apply, restore as an approved change, weekly restore drills. Applying without approval in sandbox environments is **not decided**: it changes note 006 and needs its own note and the maintainer's yes.
6. **More targets:** Kubernetes (Helm), then Google Cloud and Azure through OpenTofu.

## Decisions taken since (2026-11-07)

The maintainer chose an enterprise focus; the open questions below are settled in [note 030](030-service-switches.md): no automatic apply anywhere, OpenTofu, our own graph layout without elkjs, coding engines later. Service switches (phase 2) are built.

## Open decisions at the time (for the record)

- Whether agents may apply changes without approval in sandbox environments (separate account, cost cap, time limit). Production stays approval-only either way.
- OpenTofu as the code generated from blueprints (recommended; MPL-2.0).
- The order of coding engines (recommended: Codex, then Copilot CLI).
- The graph layout library if the current layered layout is not enough: dagre (MIT) is allowed; elkjs is EPL-2.0, which CLAUDE.md does not list.

## Rules that do not change

- Anything that changes the outside world is a proposal that a different administrator approves.
- All markup comes from `h()`: no `innerHTML`, no inline styles.
- Nothing here adds an endpoint or widens what a role can read.

## What was checked, and what was not

Checked by tests under Node: who is asked to approve (an administrator, for others' proposals only; never
a developer; nothing without a session), which runs, findings and connections are listed, the order, and
that unreadable sources give no tile; the sidebar per role and the active-page rule. Removing the "not
your own proposal" rule, and then the "administrators only" rule, each made the tests fail.

Checked in a browser: see the pull request. Not checked: a screen reader on the Needs you list.
