---
title: Workflow canvas
parent: Guides
nav_order: 43
---

# Workflow canvas

The canvas is where you draw a graph [workflow](workflows.md), n8n-style, and where a run is drawn afterwards. It fills the
page. You add nodes from a searchable side panel, wire them, and set each in a panel beside the canvas.

Use it to build or change a workflow, and to see what happened in a run.

## At a glance

| | |
| --- | --- |
| Where in the console | **Workflows → New workflow**, or **Edit on the canvas** on an existing one |
| Needs | [Agents](agents.md) for the agent nodes |
| Design note | [044](../design/044-agent-graphs.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See a graph and its runs | Developer | |
| Save a graph version | Administrator | At most 40 nodes and 80 wires; one start; every node reachable |

## How it works

1. Choose **New workflow**. Press a node's **+** (or **Add node**) and pick from a searchable side panel of your agents,
   the logic nodes and the lookups. The new node is wired in after the one you came from (between it and the node that
   followed). A node with two wires that name no branch starts both next nodes together.
2. Drag from a node's port to another node to wire them. Pick a node to set it in the panel; remove it with **Remove node**
   in its panel, or press Delete (a clicked node keeps the keyboard focus; the arrow keys move it).
3. Drag the background to move, scroll with Ctrl (Cmd on a Mac) to zoom. **Fit** shows the whole graph; the grid button
   lays it out left to right.
4. **Settings** holds three tabs: **About**, **Trigger** (by hand, on a schedule, from a webhook, after failed pipeline
   runs, and when a CloudWatch alarm goes off, each a card) and **Permissions** (developer or viewer).
5. Add a note on what changed and choose **Create workflow** (the first time) or **Save version**.

![A workflow as a graph, with a node's panel]({{ '/console/workflow-canvas.png' | relative_url }})

### Nodes

| Node | What it does |
| --- | --- |
| **Agent** | An agent does a task; it can write named fields (such as `severity`) into the run's shared state |
| **Lookup** | One read-only lookup, with no model |
| **Decide** | Takes one wire, by a rule on what earlier nodes wrote or by an agent choosing; needs an `else` wire |
| **Ask a person** | Waits for an answer or an approval |
| **Merge** | Waits for parallel branches |
| **End** | Finishes the run, as **Done** or **Failed** |

Loops are allowed: each node runs at most its limit (3 unless you say up to 10), and a run at most 50 steps.

### A node's panel

The panel has the node's name and kind at the top (click the name to change it) and tabs with icons, one part at a time. A
number on a tab says how many things are set there.

| Tab | What it holds |
| --- | --- |
| **Agent** | The agent as a card with its model, skills and tools; **Try it** chats with it in a playground with the node's task filled in; a link to change it in the builder |
| **Input** | The task, and earlier nodes' answers to hand it, as chips you switch on |
| **Output** | Where its answer is kept, and short fields to pull out of it, as chips: type one and press Enter |
| **Next** | Each wire as a card with the node it leads to and its branch |
| **Limits** | How many times the node may run |

Other nodes have their own tabs: an **Ask a person** node has **Question** (the question, its choices and **Who may answer**),
**Next** and **Limits**; an **End** node has **Ending** (Done or Failed, and a summary for the run list). A Decide node chooses
**by a rule** or **by an agent**. An Ask node's choices and the people who may answer are chips; with nobody named, any
administrator may answer.

## Read a run

A run is drawn on the same canvas: nodes turn green or red, the wires taken are bold, and picking a node shows what went in
and what came out each time it ran. A question waiting for you is answered at the top of the run. Agent answers show as
formatted Markdown.

## Security model

- Only an agent's own tools at the **Proposes** level can propose changes, and a different administrator approves them.
- Lookup nodes call one read-only lookup with no model.
- Agents read the trigger and earlier answers as data.

## Limits and verification

{: .warning }
**Checked through the console on 2026-11-27**, on the UAT instance at commit `487f373`, as an administrator in a real browser:
**New workflow**, nodes added from the side panel (an agent and two **Ask a person** nodes), the Agent, Input, Output, Next and
Limits tabs and the Question tab, the **Settings** tabs About, Trigger (five cards) and Permissions, **Create workflow**, then
**Edit on the canvas**: a wire dragged from one node to another, a node removed, the "Save your changes?" box on leaving, and
**Save version**. The graph ran with the real model and the questions were answered as in [Workflows](workflows.md). **Try it**
opened the playground with the node's task filled in; sending it was not run. The page matched the picture. Save refusals
for 41 nodes, an unreachable node and a decision without `else` came back as in Troubleshooting. Not run: Decide, Merge and
Lookup nodes, loops, and the 80-wire limit. See the [test note](../testing/2026-11-27-uat-workflows-teams.md).

**Graphs were checked with the real model on 2026-11-17**, on a graph of 12 nodes with lookups, a merge, a rule decision, a
question and an agent decision ([test note](../testing/2026-11-17-agent-graphs.md)). That check found that a lookup given
`limit` as text failed; the error now says what the tool takes, and the canvas shows it beside the arguments.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| Save is refused | The message says which: "a graph has at most 40 nodes" or "80 wires", "a graph has exactly one start", "node x cannot be reached from the start", `node x: a decision needs an "else" wire for when nothing matches`, "node x: a merge waits for at least two wires", "node x reads \"k\", which no node writes". |
| A lookup node fails at once | Its arguments are not valid JSON for the tool; check the tool's arguments. |
| A run stops at a node | "node x ran its most times (3); a loop may not end" (the limit is 3 unless set up to 10), or the run reached 50 steps. |
| The browser asks about unsaved changes | You are leaving the canvas without saving: save and leave, discard, or keep editing. |

## Related

- [Workflows](workflows.md)
- [Agents](agents.md)
- [Schedules](schedules.md)
