---
title: Agent builder
parent: Guides
nav_order: 37
---

# Agent builder

The builder is where you make and edit an [agent](agents.md). It shows what the agent is made of, as parts, with the agent
to talk to beside it. **New agent** and **Edit** open it.

Use it for any change to an agent's instructions, model, skills or tools. Every save is a new version.

## At a glance

| | |
| --- | --- |
| Where in the console | **Agents → Agents → New agent** or **Edit** |
| Needs | The developer or administrator role; a model |
| Design note | [045](../design/045-agent-studio.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| Open the builder and save a version | Developer | The tool list holds only what your role may use. A version a developer saved runs with at most a developer's rights, whoever runs it ([why](../design/054-developers-write-agents.md)). |
| Talk to the agent | Developer | On the agent's own page, not in the builder; it talks to the saved version as you |

## How it works

1. The builder lists the parts: instructions, skills, MCP servers, tools and guardrails.
2. **Add** or **Change** on a part, or the model in the header, opens one side panel with a tab for each:
   - models grouped by provider;
   - skills, with the version each is pinned to;
   - the tools an administrator turned on for each [MCP server](mcp-servers.md);
   - Forgeline's own tools in plain words;
   - guardrails: how much it may do, and the rules every agent has, shown locked.
3. Each tab has search and filters, and says whether a tool reads or asks a person to approve.
4. Talking to the agent beside the builder uses the **saved** version. Save to try your changes.

![The agent builder]({{ '/console/agent-builder.png' | relative_url }})

## Write instructions

The instructions are written in sections, each with an icon and a hint:

- **Who it is**
- **Goal**
- **Steps** (one per row, in order; Enter adds the next, the arrows move one)
- **Rules**
- **Output** (with presets: short summary, bullet points, table, JSON)
- **When to stop**
- **Examples**, **Background**, or a section of your own from **Add a section**

Leave out what does not matter; empty sections are not written. The sections make one Markdown text, a `##` heading per
section. **What the model receives** shows exactly that text, and **Markdown** lets you edit it directly. Switching between
the two changes nothing. Advisory checks help; they do not block a save.

## Unsaved changes

Leaving the builder (or the workflow canvas) with unsaved changes asks first: save and leave, discard, or keep editing.

## Security model

- The model receives exactly the one text the instructions make.
- A tool or a higher tier needs a new saved version, which is audited.
- The playground runs as you with real read tools. A T1 agent's proposal there is shown as a draft marked as such and never
  filed.

## Limits and verification

{: .warning }
**Run on the UAT instance, in part.** On 2026-11-27 (main at `487f373`) the builder was not opened in a browser: it is for administrators and the test sign-in is a developer. A developer's refusal was checked. See the [note 2026-11-27](../testing/2026-11-27-uat-assistant-agents.md).

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| The agent beside the builder ignores my edit | It talks to the saved version; save first. |
| A section is missing from the Markdown | Empty sections are not written. |
| A tool is not in the list | An administrator has not turned it on for that MCP server, or this server lacks the plugin. |

## Related

- [Agents](agents.md)
- [Agent bundles](agent-bundles.md)
- [Evals](evals.md)
- [MCP servers](mcp-servers.md)
