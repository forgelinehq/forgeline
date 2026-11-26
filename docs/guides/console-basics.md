---
title: Console basics
parent: Guides
nav_order: 11
---

# Console basics

The web console is where you do almost everything in Forgeline. This page covers how it is laid out: the sidebar and
its spaces, the search box, the Settings area, what each role sees, the dark theme and phones.

Read it first if you are new to the console. The other guides assume you can find a page by name.

## At a glance

| | |
| --- | --- |
| Where in the console | The sidebar, the top bar, and **Settings** at the bottom of the sidebar |
| Needs | A sign-in. Pages you may not open are left out of the menus |
| Design note | [Note 048](../design/048-console-structure-icons-and-editors.md), which replaced the layout of [note 029](../design/029-console-spaces-and-home.md) |

## Who can do what

The role decides which tabs and Settings pages appear. The three roles are viewer, developer and administrator (admin).
See [People and roles](people-and-roles.md) for what each may do.

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| Home, Approvals (to read), Pipelines, Map, Services, Infrastructure, Kubernetes, Findings | Viewer | |
| Canvas, Environments, Blueprints, Resources, Runs and traces, Memory, Connections, Secrets (names only), Approval rules, Policy packs | Developer | |
| Audit log, People and roles, Sign-in (SSO), MCP servers, Agent registries, OpenTelemetry, Alarm intake, Image builds | Administrator | |
| Personal API tokens | Viewer | At most your own role; needs a browser session |
| Use search | Any signed-in role | It lists only what your role may open |

## How it works

1. The sidebar has seven spaces, named after what you look after: **Home** (what needs you), **Agents**, **Workflows**,
   **Delivery** (pipelines), **Cloud** (what should exist and what does), **Operations** (how it is running) and
   **Security** (findings, approval rules, policy packs and the audit log).
2. Each space has tabs at the top, each with an icon. **Settings** is separate, at the bottom of the sidebar, and holds
   setup.
3. **Approvals** is also a button at the top right of every page, with the number of changes waiting.
4. A space or tab you may not open is left out. Old addresses still work.

| Space | Tabs |
| --- | --- |
| Home | Today, Approvals |
| Agents | Agents, Teams, Library, Usage and budgets |
| Workflows | Workflows, Schedules |
| Delivery | Pipelines, GitHub Actions |
| Cloud | Canvas, Environments, Blueprints, Resources, Map |
| Operations | Services, Infrastructure, Kubernetes, Missions |
| Security | Findings, Approval rules, Policy packs, Resource types, Audit log |
| Settings | Setup, People and roles, Sign-in (SSO), Connections, Secrets, MCP servers, Agent registries, Image builds, Models and providers, Assistant model, Memory, API tokens |

The Agents and Workflows spaces and the Runbooks and Evals tabs have their own guides; see [Related](#related).

## Find anything with search

1. Press **Ctrl K** (**⌘K** on a Mac), or click the search box at the top.
2. Type part of a name: a page, an agent, a workflow, a team, a skill, a pipeline, a mission or a blueprint.
3. Move with the arrows, open with Enter, close with Escape.

Search also offers shortcuts such as **New agent** and **New workflow**, and only lists what your role may open. The
lists of agents, workflows and the rest are read when the box opens and kept for a minute, so something created a few
seconds ago may not show yet.

![The search box, opened with Ctrl K and the letters "sec"]({{ '/console/console-basics-search.png' | relative_url }})

## Settings

Setup lives behind **Settings**. The page groups its entries as **Workspace**, **Connections**, **AI** and
**Developer**, each with a one-line note. Day-to-day work stays in the sidebar.

![The Settings page with its four groups]({{ '/console/console-basics-settings.png' | relative_url }})

## Dark theme and phones

The moon button at the top switches to the dark theme. The console remembers your choice in the browser. Every page
works on a phone.

## Security model

- The console builds its pages from text, never from HTML you supply, and its content security policy blocks inline
  styles.
- The menus follow your role, but they are not the check. Every request is checked again on the server for the role it
  needs.
- Requests that change something must send the header `X-Forgeline-Request: 1`, and cookies are `HttpOnly` and
  `SameSite=Strict` (see [People and roles](people-and-roles.md#security-model)).

## Limits and verification

The role rules for each tab are in `internal/server/ui/lib/nav.js` and `settingsnav.js` and are tested under Node.
Screenshots were taken from a real browser on stand-ins for AWS, GitHub and Claude with demo data.

{: .note }
Some older screenshots in the guides still show earlier sidebar names (Build, Ship, Team, Watch). The spaces above are
the current ones.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| A tab or Settings page is missing | Your role is below its minimum. Ask an administrator. |
| Ctrl K does nothing | A dialog is open, or you are on the sign-in page. Close the dialog first. |
| Search does not list a new agent or pipeline | The lists are cached for a minute. Wait and open it again. |

## Related

- [Home and approvals](home-and-approvals.md)
- [Connections and AI access](connections-and-ai-access.md)
- [People and roles](people-and-roles.md)
- [Assistant](assistant.md)
- [Runbooks](runbooks.md)
