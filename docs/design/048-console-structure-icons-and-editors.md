# 048: Console structure, icons and section editors

Status: **built 2026-11-20** (steps 1 to 4). On 2026-11-20 the maintainer said the menus mislead ("not sure
what is where"), accepted the proposed spaces with **Workflows as a space of its own**, and asked that the agent's
instructions and the settings panels stop being "just an input box", and that the console get proper icons. This note
replaces the spaces of note 029, frame 1.

## Question

The sidebar named spaces after verbs (Build, Ship, Team, Watch) that a newcomer cannot map to things, and one subject
lived in several places: Team held the AI agents while people were in Settings; Design, Environments, Blueprints and
Built were four words for "my cloud"; security was in Watch, in Settings and in Home. The agent's instructions were one
Markdown box, the panels long lists of fields, and the icons hand-drawn paths plus letters in boxes ("F", "Lb").

## Decisions

### 1. Spaces named after what people look after

| Space | Tabs |
| --- | --- |
| Home | Today, Approvals (with agent pushes) |
| Agents | Agents (and skills), Teams, Library, Usage and budgets |
| Workflows | Workflows (and their runs), Schedules |
| Delivery | Pipelines, GitHub Actions |
| Cloud | Canvas, Environments, Blueprints, Resources, Map |
| Operations | Services, Infrastructure, Kubernetes, Missions |
| Security | Findings, Approval rules, Policy packs, Resource types, Audit log |
| Settings | Workspace (Setup, People and roles, Sign-in), Connections (Connections, Secrets, MCP servers, Agent registries, Image builds), AI (Models and providers, Assistant model, Memory), Developer (API tokens) |

- Only labels and grouping change. Every page keeps its address and its role check, so old links and bookmarks work.
- Rules, packs, resource types and the audit log leave Settings: they are used every day by security people, not set
  once. Usage and budgets leaves Settings for Agents, where the agents are.
- A viewer sees Home, Delivery, Cloud (the map), Operations and Security; Agents and Workflows need a developer.
- The assistant is told the new places, and the page it is on is named the new way ("Agents › Teams", "Workflows").
- Later, with the new pages of the product direction: Agents › Runs and traces, Agents › Evals, Delivery › Supply
  chain, Operations › Health and Alerts, Security › Agent inventory and AI guardrails, and a search box (⌘K) that finds
  any page or thing by name.

### 2. One icon set: Lucide

- [Lucide](https://lucide.dev) (ISC license, compatible with Apache-2.0): about 1,600 consistent icons on a 24 by 24
  grid with a round stroke, the set many developer tools use.
- Not loaded at run time and not a JavaScript dependency: `scripts/icons.sh` downloads one pinned release
  (`lucide-static` 1.70.0, chosen because it was at least two weeks old, as this project now asks of new versions),
  checks its sha256, and `scripts/icons` writes only the icons the console names into `ui/lib/icons.js`, with the
  license. Adding an icon is a line in `scripts/icons/main.go` and a rerun.
- The console's old names (`home`, `flow`, `gear`…) map to Lucide icons, so every page works unchanged. Each space,
  tab, Settings entry, agent part and instruction section has its own icon. Providers keep their own marks
  (`brandMark`), since those are brands.

### 3. Sections instead of a box

- **An agent's instructions** are written in sections: who it is, goal, steps, rules, output, when to stop, and
  optional examples, background or sections of your own. Steps, rules and stop cases are lists (a row each, Enter adds
  the next, with up, down and remove); output has presets (short summary, bullet points, table, JSON). Empty sections
  are left out.
- They are still **one Markdown text**: each section is a `##` heading (the opening text has none). "What the model
  receives" shows it exactly, and **Markdown** mode edits it directly. Any text reads into sections and writes back
  unchanged (`ui/lib/instructions.js`, tested): an unknown heading becomes a section of its own, a list that is not a
  plain list stays text, a heading inside a code block is not a heading.
- **Workflow nodes** (step 3, built): the panel is tabs with icons (Agent, Input, Output, Next, Limits; Choose for a
  Decide node, Question for Ask, Ending for End) instead of one long column, each with a count of what is set.
  Earlier answers to hand an agent are chips to switch on; output fields, an Ask node's choices and who may answer are
  chip inputs; Decide by a rule or an agent, and Done or Failed, are cards; Next shows each wire as a card with the node
  it leads to. Tasks are passed as written: the server has no placeholders in a task, so earlier answers stay data the
  agent reads, never text spliced into its orders. The settings drawer is tabs too: About, Trigger (by hand, schedule,
  webhook, failed runs, each a card) and Permissions (developer or viewer as cards). Shared controls are in
  `ui/lib/controls.js`.

## Order of work

1. Spaces and Lucide icons, with tabs and Settings entries showing their icon. **Built.**
2. The instructions editor in sections, with real icons on the agent's parts and in the Markdown toolbar. **Built.**
3. Workflow node panel and settings drawer in tabs with chips. **Built.**
4. The search box: Ctrl K, ⌘K or /, or the box at the top. It finds pages, agents, workflows, teams, skills,
   pipelines, missions and blueprints, and offers shortcuts (New agent…), only what the role may open: pages from the
   menus the role sees, things from the lists the API lets it read (`ui/lib/palette.js`, tested). **Built.** The new
   pages come with their features.
