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
| Home, Approvals, Pipelines, Map, Services, Infrastructure, Kubernetes, Findings | Viewer | |
| Every tab of Agents and Workflows, GitHub Actions, the Cloud tabs except Map, Missions, Runbooks, Approval rules, Policy packs, Resource types | Developer | |
| Settings (the gear), and in it Setup, Connections, Secrets, Models and providers, Assistant model, Assistant permissions, Memory | Developer | A viewer has no Settings link at all |
| Audit log; Settings: People and roles, Sign-in (SSO), MCP servers, Agent registries, OpenTelemetry, Alarm intake, Image builds, Step container limits | Administrator | |
| Personal API tokens | Viewer | At most your own role; created only from a browser session, not with a token. Viewers reach them through the user menu (**Account & password**) |
| Use search | Any signed-in role | It lists only what your role may open |

## How it works

1. The sidebar has seven spaces: **Home**, **Agents**, **Workflows**, **Delivery** (pipelines), **Cloud**,
   **Operations** and **Security**. A space with no tab your role may open is left out.
2. Each space has tabs at the top, each with an icon; the tabs show only when the space has two or more you may open.
   **Settings** is separate, at the bottom of the sidebar, and holds setup.
3. **Approvals** is also a button at the top right of every page, with the number of changes waiting (AI proposals,
   cloud changes, and for developers and up, agent pushes and runbook fixes; it refreshes every minute).
4. The sidebar link of a space opens its first tab. Old addresses still work.

| Space | Tabs |
| --- | --- |
| Home | Today, Approvals |
| Agents | Agents, Teams, Runs and traces, Evals, Library, Usage and budgets |
| Workflows | Workflows, Schedules |
| Delivery | Pipelines, GitHub Actions |
| Cloud | Canvas, Environments, Blueprints, Resources, Map |
| Operations | Services, Infrastructure, Kubernetes, Missions, Runbooks |
| Security | Findings, Approval rules, Policy packs, Resource types, Audit log |

Settings has four groups. **Workspace**: Setup, People and roles, Sign-in (SSO). **Connections**: Connections, Secrets,
MCP servers, Agent registries, OpenTelemetry, Alarm intake, Image builds. **AI**: Models and providers, Assistant
model, Assistant permissions, Step container limits, Memory. **Developer**: API tokens.

Most spaces and tabs have their own guides; see [Related](#related) and the [guide index](index.md).

## Find anything with search

1. Press **Ctrl K** (**⌘K** on a Mac), press **/** when no text box has focus, or click the search box at the top.
2. Type part of a name: a page, an agent, a workflow, a team, a skill, a pipeline, a mission or a blueprint. Letters
   in order also match ("wfs" finds Workflows).
3. Move with the arrows, open with Enter, close with Escape.

With nothing typed it lists pages and shortcuts. The shortcuts are **New agent** and **New workflow** (administrators),
**New pipeline** and **Add a model provider** (developers), and **Add an MCP server** (administrators). Search lists only
what your role may open. The lists of agents, workflows and the rest are read when the box opens and kept for a minute,
so something created a few seconds ago may not show yet. A viewer finds pages and pipelines only.

![The search box, opened with Ctrl K and the letters "sec"]({{ '/console/console-basics-search.png' | relative_url }})

## Settings

Choose **Settings** at the bottom of the sidebar (developers and administrators). The page groups its entries as
**Workspace**, **Connections**, **AI** and **Developer**, each with a one-line note, and every Settings page shows the
same menu on its left. Day-to-day work stays in the sidebar.

![The Settings page with its groups]({{ '/console/console-basics-settings.png' | relative_url }})

## Dark theme and phones

The moon button at the top (a sun in the dark theme) switches the theme. The console remembers your choice in the
browser. On a narrow screen the sidebar hides behind the menu button at the top left.

## Security model

- The console builds its pages from text, never from HTML you supply, and its content security policy blocks inline
  styles.
- The menus follow your role, but they are not the check. Every request is checked again on the server for the role it
  needs. A page you may not open shows "You do not have access to this page".
- A request from a browser session that changes something must send the header `X-Forgeline-Request: 1` and, if it
  has an `Origin`, one that matches the server. The session cookie is `HttpOnly` and `SameSite=Strict` (see
  [People and roles](people-and-roles.md#security-model)).

## Limits and verification

The role rules for each tab are in `internal/server/ui/lib/nav.js` and `settingsnav.js` and are tested under Node.
This page was checked against those two files, `app.js` and `lib/cmdk.js` and `lib/palette.js`.

{: .warning }
**Checked in a browser on 2026-11-27**, on the UAT instance (main at `487f373`), as a developer and as a viewer: every space and its tabs against the table, Ctrl K, `/`, a no-match search, the Settings groups, the theme (it survived a reload), the phone menu button, the "You do not have access to this page" page, and the viewer refusals. The two screenshots were taken from a throwaway local server with demo data; the search picture shows an administrator's list (it includes **Audit log**, which a developer does not see). **Not checked.** A screen reader, and a real phone.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| A tab or Settings page is missing | Your role is below its minimum. Ask an administrator. |
| "You do not have access to this page" | You opened an address your role may not use. |
| Ctrl K does nothing | A dialog is open, or you are on the sign-in page. Close the dialog first. |
| Search shows "Nothing matches" | Try a shorter word. Things you may not open are never listed. |
| Search does not list a new agent or pipeline | The lists are cached for a minute. Wait and open it again. |
| Creating an API token fails with "sign in to the console as a person" | Tokens can only be created from a browser session, not with another token. |

## Related

- [Home and approvals](home-and-approvals.md)
- [Connections and AI access](connections-and-ai-access.md)
- [People and roles](people-and-roles.md)
- [Assistant](assistant.md)
- [Runbooks](runbooks.md)
