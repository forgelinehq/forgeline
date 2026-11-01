# 022: Console redesign

Status: **Step 1 and step 3 built; steps 2 and 4 need their own notes.** Date: 2026-11-01.

## Question

The console grew one page at a time (note 006) and now shows it: a flat list of 14 pages, an
Infrastructure page that has a tab per concern but no view of the resources themselves and no
logs, agent and skill instructions typed into a plain box, and an assistant that is a chat
panel which can only reply with text. How should it look and work, without a rewrite and
without weakening any rule from notes 006 and 008?

A clickable prototype (example data only) was reviewed before this note. It is a design aid,
not code, and is not part of the repository.

## Decisions

| Decision | Choice |
| --- | --- |
| Navigation | Group the pages by the job a person is doing: Ship, Cloud, AI, Govern. Same routes, same role checks |
| Colour | One brand colour (ember) replaces indigo, set once in `web/tailwind.css`. Every contrast pair used for text on the brand colour is at least 4.5:1 |
| Instructions | Agent instructions and skill steps are edited as Markdown: toolbar, live preview, and advisory checks. They are stored and sent to the model as the same plain text as before, so the API and versions do not change |
| Markdown rendering | A small parser that returns blocks, then DOM built with `h()`. No links, no images, no raw HTML. Text that looks like HTML is shown as text |
| Checks | Advisory only. The server's validation stays the authority. They never block a save |
| Infrastructure | A map of the account's resources with a detail drawer and a log viewer. Needs a read-only log endpoint, so it gets its own note (step 2) |
| Assistant | Becomes an agent surface that renders plans, tool calls, results and approval requests as typed cards. Needs an event stream and a card schema, so it gets its own note (step 4) |

## Rules that do not change

- All markup comes from `h()`. No `innerHTML`, no inline styles; the existing test enforces it.
- Cloud changes still only happen through proposal and approval. An approval card in the
  assistant is a link to, and a view of, the same proposal; it adds no second path.
- The assistant keeps its read-only role and the lookups from note 015.
- Local first: nothing here needs a font, script or image from another host (the CSP is
  `'self'`), so no web fonts.

## Order of work

1. **Shell and colour (built).** Grouped navigation and the ember palette. No page behaviour
   changes.
2. **Infrastructure map and logs (needs a note).** Open questions: which log source (CloudWatch
   Logs through the existing read-only AWS access, or container logs for Forgeline's own
   steps), how a live tail is paged and bounded, and what is redacted (`internal/redact`).
3. **Markdown editor (built).** `ui/lib/markdown.js` (parser and checks, no DOM),
   `ui/lib/mdview.js` (rendering), `ui/lib/mdeditor.js` (editor). Used for agent instructions
   and skill steps, and to show them on the detail pages with a raw-text toggle.
4. **Agent surface (needs a note).** Open questions: one server-sent-event endpoint and its
   event types, the card types (plan, tool call, chart, proposal), how an approval card refers
   to a proposal, and how a run that is waiting for approval resumes.

## What was checked, and what was not

Checked by tests: the Markdown parser's blocks, that HTML-looking input stays text, and the
instruction checks (including that removing a check makes its test fail).

Not checked: the pages in a browser by a committed test (the console has none; see the
console-hardening item in `docs/PLAN.md`), and any screen reader behaviour of the editor
beyond labelled controls.
