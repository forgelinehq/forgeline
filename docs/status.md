---
title: Status
nav_order: 5
---

# Status

Pre-alpha. Built and tested, with the race detector, real Docker and real browsers:
pipelines and runs, the console, secrets, roles and audit, GitHub triggers, agent steps, the
assistant, AWS sign-in, the GitHub App.

**Checked on real services:** AWS sign-in, an approved deploy and delete of a stack on a real account, a GitHub App created and installed on github.com, and the assistant on a real Claude Code login token.

**Not checked against the real service:** a real GitHub webhook delivery and commit status; the assistant's API-key mode against the real
Anthropic API; TLS behind a real proxy; more than one server sharing a database; single sign-on (not built).

**Console redesign:** grouped navigation, the ember colour, the Markdown instruction editor and the AWS map are built, and so are CloudWatch alarms on the map ([note 022](https://github.com/forgelinehq/forgeline/blob/main/docs/design/022-console-redesign.md)). Designed but **not built**: the log viewer and metrics ([023](https://github.com/forgelinehq/forgeline/blob/main/docs/design/023-infrastructure-map-and-logs.md)) and the assistant as an event stream ([024](https://github.com/forgelinehq/forgeline/blob/main/docs/design/024-assistant-agent-surface.md)). The redesign was checked in a browser on a fake AWS account, not on a real one; the alarms in particular have not been read from real CloudWatch.

Roadmap and plan: [ROADMAP](https://github.com/forgelinehq/forgeline/blob/main/docs/ROADMAP.md), [PLAN](https://github.com/forgelinehq/forgeline/blob/main/docs/PLAN.md), [ARCHITECTURE](https://github.com/forgelinehq/forgeline/blob/main/docs/ARCHITECTURE.md).
