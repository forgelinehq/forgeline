# 044: Agent graphs

Status: **Parts 1 and 2 built (2026-11-17): the engine and the canvas.** Builds on workflows (Team › Agents › Workflows: stages of agents run in order, with
gates), agents and their tools (note 024), proposals and approval rules (notes 016, 032), budgets (036), missions (037)
and the library (040, step 8).

## Question

The maintainer asked for agent workflows "like LangGraph but with visuals like n8n", and for checking different agents
together. Today a workflow is a straight line: up to six stages, up to four agents side by side in each, an optional
gate before a stage, and each stage hands the text of the earlier ones to the next. It cannot branch on what an agent
found, try again until a check passes, wait for a person's answer in the middle, run a fixed lookup without a model,
or be drawn and edited as a graph.

What should a graph workflow be in Forgeline, and how does it stay inside the trust rules: pinned agent versions,
nothing written except through proposals a different administrator approves, never above the role of whoever started
it, budgets, and an audit of every step?

## What the two references do

- **LangGraph** ([docs](https://langchain-ai.github.io/langgraph/)): a graph of nodes that read and update one shared
  **state**; **conditional edges** choose the next node from the state; **cycles** are allowed with a recursion limit;
  **checkpoints** save the state after every step, so a run can stop at an **interrupt** (a person's input) and resume;
  parallel branches run in the same step and their updates are merged by the state's reducers.
- **n8n** ([docs](https://docs.n8n.io/)): a canvas of nodes joined by wires; **triggers** start a run (by hand,
  schedule, webhook, an app event); **IF / Switch** nodes branch; **Merge** waits for branches; **Wait** pauses for a
  time or a person; every run is kept as an **execution** where each node shows the data that went in and came out.

Forgeline takes the model from LangGraph (state, conditional edges, bounded cycles, checkpoints, interrupts) and the
experience from n8n (a canvas, triggers, node-by-node executions you can open).

## Decisions

| Decision | Choice | Why |
| --- | --- | --- |
| What a graph is | A workflow version holds `graph`: `nodes` (each with an id, a kind, a position and its settings) and `edges` (`from`, `to`, and for a branch the `when` label it takes; a branch may wire to several nodes, which start together). The old `stages` form still saves and runs on its own engine; **Edit on the canvas** turns it into a graph (a column per stage, a question before a gated stage, a merge after parallel steps) saved as a new version | Nothing existing breaks |
| Node kinds | **Start** (by hand or on a schedule; webhooks later), **Agent** (a pinned agent version and a task), **Lookup** (one read-only lookup with fixed arguments, no model: `list_runs`, `guard_findings`, `blueprints_built`...), **Decide** (branch on the state by a rule, or by asking an agent to pick one of the edge labels), **Ask a person** (a question; an allowed person answers or approves, and the answer goes into the state), **Merge** (waits for every branch that reaches it), **End** (finishes the run, succeeded or failed, with a summary) | Covers LangGraph's nodes and conditional edges and n8n's IF, Merge, Wait; Lookup is n8n's "app node" with Forgeline's own read tools |
| State | A map of named values. Every node's output is saved under its id (`state.<node>`), as text up to 6 KB. An Agent node may also be asked for declared fields (`fields: ["severity", "service"]`): it answers with `<state>{...}</state>`, which is checked (known keys, strings or numbers, short) before it is merged | LangGraph's state with one simple reducer (last write wins per key); declared fields keep branches deterministic |
| How state reaches an agent | Its task, then the values it names (`reads`), inside a data block that says it is data and never instructions, as workflows do today | Untrusted text never becomes an instruction |
| Branching | A Decide node by **rule** compares one state value with a constant (`equals`, `contains`, `>`, `<`, `is empty`), first match wins, with an `else` edge required. By **agent**: a pinned agent is asked to answer one of the edge labels exactly; anything else is retried once, then the `else` edge | Rules for what can be rules; a model only where judgement is needed, and its answer is constrained |
| Cycles | Allowed. Each node runs at most `max_visits` times (default 3, at most 10), a run at most 50 node steps; past either, the run fails saying which limit | LangGraph's recursion limit, made visible per node |
| Parallel | A node with several plain outgoing edges starts them all; branches run at most 4 at a time; a Merge waits for every branch that was started towards it. Only Merge may have more than one incoming edge from parallel branches; any node may be re-entered by a loop | n8n's Merge; no hidden joins |
| People in the loop | Ask a person: who may answer (any administrator, or named people), the question, optional choices (which become the edge labels). The run waits (as gates do today, up to 7 days, then fails). Answers are audited with the name | LangGraph's interrupt, n8n's Wait for a form |
| Writes | Unchanged: only an Agent node whose agent is T1 can file proposals, through its own tools; a different administrator approves them under the approval rules. No node kind writes by itself | The trust model does not move |
| Different agents | Each Agent node names its own agent and version. Agents gain an optional **model** (from the models the connected backend offers; empty uses the default), so a graph can mix a quick model for routing with a stronger one for analysis | What "checking different agents" needs; budgets (036) already count per agent |
| Checkpoints | The run is saved after every node: the state, each node's visits, input and output. A restart fails a node that was running (as today) and keeps waiting nodes waiting | n8n's executions; no blind resume |
| Who | As workflows: developers build and start; a run is never above its starter's role and never above developer when scheduled; administrators answer gates unless a node names people | Same rules |
| Limits | At most 40 nodes and 80 edges per graph; a task at most 2,000 characters; at most 20 declared fields | Bounded cost and clutter |

## The editor and the run view

- **Team › Workflows › a workflow** opens a canvas: nodes as cards (icon, kind, name, one line of what it does), wires
  from the right port to the left port, labels on branch wires. A palette adds nodes; dragging moves them; dragging from
  a port draws a wire; selecting a node opens its settings beside the canvas (agent and version, task, reads, fields;
  rule; question and who answers). The graph is checked as it is edited, with the problem shown on the node.
- **Saving** makes a new version, as today, with the diff in words.
- **A run** is drawn on the same canvas: the node running pulses, finished nodes are green or red, the wires taken are
  drawn bold with moving dots, waiting nodes ask for their answer in place. Selecting a node shows, for each visit, what
  went in (the task and the state it read) and what came out, as n8n does.
- The layout code is pure functions tested under Node, like `wfgraph.js` and the blueprint canvas.

## Parts

1. **The graph and its engine**: the model, checks, the stages-to-graph conversion, the engine (state, rules, cycles,
   merge, checkpoints, ask a person), the API, tests with a scripted model. Mutation-tested limits.
2. **The canvas**: edit and run views as above.
3. **Different agents**: the per-agent model, Decide by agent, Lookup nodes for every read tool.
4. **Triggers**: a signed webhook (GitHub events and a generic one), then events from Watch (an alarm, a failed run).

Each part is checked on the Pi with the real model before the next.

## Built and checked (parts 1 and 2)

- The engine (`console_ai_graph.go`): checks (one start, every node reachable, nothing into the start or out of an
  end, a decision's `else` wire, wires for every rule branch and choice, reads of keys some node writes, read-only
  lookups only, answerers who are active developers or administrators), and the run loop. Each step runs the ready
  nodes, at most four at a time. It merges what they wrote into the state, last write wins per key and only declared
  fields. It then finds the next ready nodes, and saves the run.
- A merge goes ahead when every wire into it has delivered, or when nothing else is left to run (branches that took
  another path never come).
- Tests with a scripted model: a rule branch on a field an agent wrote, with undeclared keys dropped; a loop that ends
  when its check passes; a node that stops at its limit; the 50-step limit; parallel branches of different lengths
  meeting at a merge that runs once; state reaching an agent only as closed data; a question answered by an
  administrator, or only by the people it names, picking its wire; a lookup without a model; an agent choosing a
  branch, with an unclear answer asked once more and then `else`; a restart.
- 28 mutations of the checks and limits, all caught.
- The canvas (`ui/lib/agraph.js`, pure and tested under Node; `ui/pages/agent_canvas.js`): editing as described above,
  wires drawn by dragging from a port, **Tidy up**, and a stages workflow converted to a graph that the server's own
  checks accept (tested). Checked in a browser: a graph with an agent, a rule decision and a second agent, drawn by
  dragging and saved.
- Not checked yet: a graph run with the real model on the Pi.

## Not decided

- Whether a graph may call another graph (LangGraph subgraphs). Not in parts 1 to 4.
- Pipeline nodes (start a Forgeline pipeline and wait for it): useful, but a pipeline runs code; it needs its own
  decision on who may start one from a graph.
- Agents from other providers or frameworks (an OpenAI model, a LangGraph program run as a step): only if the
  maintainer asks; the graph format does not prevent it.
