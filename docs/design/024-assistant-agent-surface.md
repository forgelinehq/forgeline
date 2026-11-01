# 024: The assistant as an agent surface

Status: **Design only. Nothing here is built.** Date: 2026-11-01.
Builds on notes 008 (assistant), 015 (AWS lookups), 006 (proposals) and 022 (step 4). Facts about the
code are from `origin/main` at `f927cad`.

## Question

The assistant is a side panel that sends one question and shows one block of text. People cannot see
what it looked up, cannot tell what it is waiting for, and cannot act on a change it proposes without
leaving the panel. How do we show its work and let people act on it, without changing what it is
allowed to do?

## What exists today

- `POST /v1/assistant/chat` is one request and one JSON answer (`ChatReply`: reply text, the context
  note, the names of lookups run, proposal ids, environment ids).
- The server runs the loop (`askWithLookups`): the model asks for a lookup by replying with one
  `forgeline-lookup` block, the server runs it as the person who asked, and returns the result as data.
  There is no native tool calling and no token streaming; each backend returns a whole answer.
- The assistant cannot change anything. A change it wants is filed as a **proposal** that a different
  administrator approves, and the existing change engine then executes it (notes 006 and 021). The
  assistant never executes.
- Replies are shown as plain text.

## Decisions

| Decision | Choice | Why |
| --- | --- | --- |
| What stays | The model protocol, the lookup tools, roles, limits, the untrusted-data rules and the audit entries are unchanged. `POST /v1/assistant/chat` stays as it is | This note changes how the answer is shown, not what the assistant can do |
| New endpoint | `POST /v1/assistant/runs`, same request body, answers with `text/event-stream`. It calls the same loop with an observer that emits events | One code path, so a rule fixed in one place holds in both |
| Transport | One streaming POST read with `fetch`. No stored run, no resume. If the connection drops the question is asked again. Closing the stream cancels the request context, which stops the model call | A question takes seconds. Stored runs would add state for no gain |
| Event names | The subset of the [AG-UI](https://docs.ag-ui.com/sdk/python/core/events) protocol that fits: `RUN_STARTED`, `RUN_FINISHED`, `RUN_ERROR`, `TOOL_CALL_START`, `TOOL_CALL_ARGS`, `TOOL_CALL_END`, `TOOL_CALL_RESULT`, `TEXT_MESSAGE_START`, `TEXT_MESSAGE_CONTENT`, `TEXT_MESSAGE_END`, and `CUSTOM` for ours (namespaced `forgeline.*`) | An open vocabulary other tools can read. The exact event list differs between versions of the protocol, so **check the current specification before building** |
| What a lookup shows | One card per lookup: the tool name, its arguments, the state (running, done, failed) and a preview of the result cut at 2 KB, as text. Only lookups the asker's role may run are ever emitted | Same data the person could already see, now visible while it happens |
| The answer | One `TEXT_MESSAGE_CONTENT` with the whole answer, rendered as Markdown with the renderer from note 022. A `yaml` block keeps its "open in the editor" action | The backends do not stream, so the first version streams steps, not words. Token streaming can follow per backend |
| **Approval** | A `CUSTOM forgeline.proposal` event with the proposal id. The card loads the proposal with the existing `GET /v1/cloud/changes/{id}`, shows its preview and status, and its Approve and Reject buttons call the existing approve and reject endpoints. The server still refuses an approver who is the proposer and anyone below administrator | The card is a view of the proposal. There is no second approval path, and the run does not wait: it ends when the proposal is filed, and the card follows the proposal's status by reading it, including "expired" (a proposal older than 24 hours is refused) |
| Context | The chips above the thread show what the page gave the assistant (the context note it already returns) and the person's role | Makes "what does it see" visible |
| Rate limit and audit | Unchanged: 20 questions a minute per person; one `assistant.chat` entry and one `assistant.lookup` per lookup | |

## What this note does not promise

The clickable prototype showed a plan checklist, a latency chart and a run that resumed after approval.
Those are **not** in the first version:

- **Plan and chart cards** need the model to return structured data. Today it returns text. That is a
  separate decision (a second fenced block type, validated like `forgeline-lookup`).
- **A run that resumes after approval** is wrong for this system. Execution belongs to the change
  engine, not the assistant. The card shows the proposal's own status.

## Events, in order for a typical question

1. `RUN_STARTED`, then `CUSTOM forgeline.context` with the context note.
2. For each lookup: `TOOL_CALL_START` (tool name), `TOOL_CALL_ARGS`, `TOOL_CALL_END`, `TOOL_CALL_RESULT`
   (preview, or the refusal reason).
3. For each proposal filed: `CUSTOM forgeline.proposal` with its id. For each environment requested:
   `CUSTOM forgeline.environment`.
4. `TEXT_MESSAGE_START`, `TEXT_MESSAGE_CONTENT`, `TEXT_MESSAGE_END`.
5. `RUN_FINISHED`, or `RUN_ERROR` with the same message the JSON endpoint would return.

## Needs a decision

1. **Adopt the AG-UI names, or our own?** Recommended: the AG-UI names for the events listed above.
2. **Token streaming** for the `api` backend only, later. Recommended: not in the first version.
3. **May the assistant read logs?** If note 023 lands, log lines are untrusted input that could carry
   instructions. Recommended: not until there is a reviewed answer to that, and never by loosening
   `call_aws`.

## Order of work

1. Refactor `askWithLookups` to take an observer. No behaviour change; the existing tests must pass
   unchanged.
2. The events endpoint, with Go tests (below).
3. The console: an SSE reader, the thread with lookup, proposal and answer cards, replacing the chat
   panel. The side panel stays as the entry point.

## Tests to write

Go, with the race detector: event order for a question with lookups, a proposal and neither; a lookup
above the asker's role is never emitted; no event carries a secret value or the system prompt; a closing
tag inside a lookup result cannot end the untrusted block (existing test, run through the new path);
closing the stream cancels the model call; the rate limit and audit entries match the JSON endpoint.
Node: the SSE frame parser as a pure function. Then remove each rule in turn and confirm a test fails.

## What was checked, and what was not

Checked: the facts above, by reading the code, and the AG-UI event names against its published SDK
reference. Nothing is built.

Not checked: how each backend behaves when its request is cancelled, whether the proxy in front of a
real deployment buffers server-sent events, and the card layout in a browser.
