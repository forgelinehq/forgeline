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

## Slice 2: the visual system, the assistant and agents (built 2026-11-07)

| Decision | Choice |
| --- | --- |
| Neutrals | Every grey is a Tailwind `slate` shade, so the scale is redefined once in `web/tailwind.css` with a warm hue. Lightness matches slate step for step, so contrast stays where it was |
| Type | Bricolage Grotesque for page titles, figures and the brand; IBM Plex Sans for the interface; IBM Plex Mono for code and section labels. Served by Forgeline from `ui/fonts` (SIL Open Font License, text in `ui/fonts/LICENSE-OFL.txt`); the CSP gains `font-src 'self'` and nothing else |
| Assistant | A welcome that says what it can do (explain, prepare, propose) and what it cannot; each lookup as a sentence with the technical detail one click away (`ui/lib/steplabel.js`), folded into "Looked at N things" when done; a composer with the send button inside; labelled header buttons |
| Agents | A face per agent, On/Off and Advises/Proposes in words, tools grouped by what they touch and named in plain words (`ui/lib/tools.js`); the editor in sections, tiers as two cards, tools and skills as check cards with what each does; the Markdown editor as one framed box |
| Tested | Under Node: every tool the server offers has a sentence and a group (a new tool without words fails the test), labels, summaries, grouping, the schema stripped from help, tiers |

## Slice 3: frame 1, five spaces and the new Home (built 2026-11-08)

The maintainer confirmed six screen frames (Home, Connections, the assistant, the design canvas, Team, Watch). Frame 1 is built:

| Decision | Choice |
| --- | --- |
| Sidebar | Five spaces only: Home, Build, Ship, Team, Watch, then Settings. A space links to its first tab the role may open; a space with nothing the role may open is left out (a viewer sees Home, Ship and Watch) |
| Tabs | The pages people know are tabs at the top of their space (`ui/lib/nav.js`). Same addresses, so old links work. Planned tabs (Design, Blueprints, Missions) are shown dimmed with "soon" and are never links. Settings keeps its own menu and no tabs |
| Approvals | A button at the top right of every page with the number of changes waiting, and a tab under Home |
| Home | Ask or tell Forgeline (opens the assistant with the question; developers and up), a greeting with how much needs you, tiles for pipelines, spend (loaded on request: AWS bills each look), the AI team and security, Needs you, Start something and Recent changes (decided proposals and changes). The ask bar sits outside the part that redraws every few seconds, so typing is never lost |
| Tested | Under Node: spaces per role, tabs per role, planned tabs never links, every page in its space, Settings in none, the greeting, the summary line, recent changes (decided only, newest first, wording, limit) |

## Slice 4: frame 2, Connections (built 2026-11-08)

| Decision | Choice |
| --- | --- |
| Layout | Settings › Connections: connected systems (worst first) and logins found on the server on the left; the one picked opens on the right with Overview, Services (AWS), What the AI may do and Activity. Nothing connected opens Add a connection |
| Add a connection | Everything not connected, including Coming soon, with filters and search; it is no longer mixed into the main view |
| Service levels | Off, Look only, Change with approval per AWS service (note 030) |
| Words | "Integrations" is "Connections" in the menu and in messages; the addresses stay `#/integrations/…` so links keep working |
| Tested | Under Node: the left column's groups (each item in exactly one), what the page opens on, the levels request |

## Slice 5: frame 5, Team (built 2026-11-08)

| Decision | Choice |
| --- | --- |
| Page | Team › Team (`#/team`), the Team space's first tab: you at the top, the agents that report to you, then theirs; a Hire an agent card for administrators; the mission strip below |
| Reporting lines | An agent may report to another agent (`reports_to`, kept on the agent, not in its versions, so a line change is not a new version). Administrators set it from the card; loops are refused, and a stored loop is shown cut rather than followed. A line describes the team only: a lead does not hand out work, and it changes nothing about what an agent may do or who approves its proposals |
| States | Read from what exists, nothing new is recorded: off (turned off), working (a step of a running workflow), waiting (in a run stopped at a gate for a person), asleep until the next scheduled run of an enabled workflow it is a step of, or waiting to be asked. With the assistant off, nothing is promised on a schedule and the page says why |
| Mission strip | The workflow run in progress, else the latest, stage by stage; a gate that waits is marked waiting for you, with a link to review it. A workflow with a run in progress promises no next time, because the scheduler skips it until the run ends |
| Not yet | Budgets and spend per agent (Forgeline does not measure spend yet, so the frame's spend bar is left out rather than invented); a lead that hands out work; missions as their own model (note 014) |
| Tested | Server: lines, loops refused (directly and further up), self, unknown agent, roles, audit, a new version keeps the line, each state, the week's counts, the mission choice, the assistant off; 11 of 12 rules caught when removed (the twelfth is an equivalent guard). Node: the tree (including a stored loop), the state and facts words, schedules, the mission steps |

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
