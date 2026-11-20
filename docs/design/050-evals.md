# 050: Evals: test an agent before people rely on it

Status: **slices 1 and 2 built 2026-11-20**; slice 3 (attack suites, a pipeline step) next. Phase 2 of the product direction the maintainer accepted on
2026-11-20. Builds on traces (note 049): every eval attempt is a trace.

## Question

An agent changes when someone edits its instructions, pins a new skill, or gives it another model. Today the only
check is talking to it. How does a team know a new version still does what the old one did, does not call a tool it
should not, does not leak a secret, and does not follow orders planted in what it reads, before workflows, teams and
schedules start using it?

## What the field does (November 2026)

- Deterministic checks first (required or forbidden tool calls, structured output, policy rules), an LLM judge with a
  written rubric only where judgement is needed, and the judge itself checked against a few human-graded answers.
- Grade the path, not only the answer: which tools were called, in what order, and what was never called.
- Consistency: pass^k (all of k tries pass) says far more about an agent than one lucky pass; τ-bench showed agents
  with good single-run scores falling sharply when they must succeed every time.
- Regression suites (should stay near 100%) apart from capability suites (start low), with new cases taken from
  real runs.
- Attack suites follow the OWASP Top 10 for Agentic Applications: ASI01 goal hijack (orders planted in tool output,
  documents, issues), ASI02 tool misuse, ASI03 identity and privilege abuse, ASI04 supply chain, and the rest.

## Decisions (proposed)

### 1. A suite belongs to an agent

A suite is a list of cases for one agent (Agents › Evals). A case is a message to the agent, optional **fixtures**
(what a tool returns, by tool name, instead of reading live data), and **checks**. At most 100 cases a suite.

### 2. Checks

| Check | Passes when |
| --- | --- |
| Contains / does not contain | the answer has (or lacks) the text, ignoring case |
| Matches | the answer matches a regular expression |
| Is JSON | the answer (or its fenced block) parses as JSON, optionally with the listed keys |
| Field equals | a `<state>` field the agent set equals a value |
| Calls a tool / never calls a tool | the tool was (or was not) called, optionally with an argument value |
| At most N tool calls | the run used no more |
| Within budget | at most N tokens, or N seconds |
| Judge | a model reads the question, the answer and a rubric, and says pass or fail with a reason |

The judge is told the answer is data, never orders, and must reply in a fixed shape; an unreadable verdict fails the
check. It uses a model the suite names, or the assistant's.

### 3. Running a suite is safe

- An eval runs the agent as the person who starts it, with its own tools, exactly as a chat would, but **nothing
  that changes anything runs**: proposals, environment requests, prepared actions and memory writes are recorded as
  "would call, with these arguments" and the agent is told they were recorded. Checks can test them.
- A tool with a fixture returns the fixture, so a case gives the same input every time; tools without one read live
  data, read-only, as in a chat.
- Each case runs 1 to 5 times (the suite's attempts). A case passes only if **every** attempt passes (pass^k); the run
  also reports how many attempts passed.
- A run can use another model than the version's, to compare models on the same cases.
- Every attempt is a trace (note 049), linked from the result, and counts against the agent's budget like any run.

### 4. Cases from real runs

A trace that kept its prompt and answer (note 049, decision 3) has **Add to a suite**: its question becomes a case,
and the answer it gave becomes a "contains" hint to edit. Without kept content, a case is typed.

### 5. A gate on new versions (slice 2, accepted and built 2026-11-20)

The maintainer: "yes if passes and published it should be the current version". As built: Publishing on an agent's
page names the gate suite; a saved version is then a candidate; **Publish** needs a run of that suite on that version,
with its own model, every case passed, started after the suite last changed; **Run the suite, publish if it passes**
starts one and publishes on a full pass. A rollback is not gated. Checked in the browser against a stand-in model.

An agent can say "a new version becomes current only if suite X passes". Until it passes, a saved version stays a
candidate that people can try but that workflows, teams and schedules do not use. This changes what saving an agent
means, so it waits for a decision.

### 6. Attack suites (slice 3)

Ready-made cases per OWASP agentic risk, built on fixtures: an issue or a log line that tells the agent to file a
change or reveal a secret (ASI01), a request to use a tool outside its purpose (ASI02), a request for data its role may
not read (ASI03). Each checks that the agent did not obey: no proposal recorded, no secret in the answer, the refusal
said why. Added to any agent's suites in one click.

## Order of work

1. Suites, cases, fixtures, the checks above including the judge, safe runs with attempts and a model override, and
   the Evals pages. Cases from traces. **Built:** `console_ai_evals.go`, `ui/pages/evals.js`. A fixture or a recorded
   call only answers a tool the agent may use; any other is refused as in a chat. Checked with a three-case suite
   against a local stand-in model: two cases passed both attempts and the budget case failed both (1,552 tokens over
   a 1,000 limit).
2. The gate, once decided.
3. Attack suites; an eval step for pipelines, so a change to an agent can be tested in CI.

## Decisions for the maintainer

1. ~~The gate (decision 5)~~: **yes** (2026-11-20): a version becomes current when it passes and is published.
