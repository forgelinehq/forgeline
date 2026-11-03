# 027: Assistant memory and conversation history

Status: **Decided 2026-11-03; slice 1 (history, notes, the `remember` tool, the Memory page) is built.**
Builds on notes 013 and 015 (the assistant and its lookups), 024 (the event stream), 026 (access policy) and 005 (threat model).

## Question

The assistant forgets everything when the tab closes, and it learns nothing about the people or the system it
works for. How do we give it a memory that makes it better over time, and give each person a history of what they
asked and what came of it, without letting untrusted text (logs, pull request titles, resource names) write to
that memory, and without one person reading another's chats?

## What exists today

- The conversation lives in the browser (`history` in `ui/lib/assistant.js`). The server keeps no chat. Only the audit log records that a question was asked, not what it said.
- `askAssistant` builds the system prompt from the fixed text, the lookups the person may use, and one `<untrusted_context>` block.
- Lookups that file something (a proposal, an environment request) report it through `lookupSink`.

## Decisions

| Decision | Choice | Why |
| --- | --- | --- |
| Where it lives | The document store (`chat` and `memory` kinds), in Go, no new dependency and no vector database | Local first (principle 4). A laptop and a server behave the same |
| History is private | A conversation belongs to the person who had it. Only they can list, read or delete it. An administrator cannot read it, and a request for someone else's returns 404 | Chats contain questions people would not ask in public. Audit already records that asking happened |
| What history keeps | The questions, the answers, the names of the lookups used, and the ids of proposals and environment requests the chat filed. **Never lookup results** | Results can hold anything the cloud returned. The answer is what the person saw and kept |
| Secrets in history | Known secret shapes (cloud keys, tokens, private keys) are replaced with `[hidden]` before storing | A pasted key must not outlive the tab |
| Limits | 100 conversations a person (oldest go first), 60 messages each, 90 days | A chat log is not an archive |
| Who is remembered | Only people. The shared server token, agents, MCP clients and plain `/chat` calls have no history and write no memory | Nobody is there to own it or to check it |
| What memory is | Short notes (up to 300 characters), either **about a person** (their preferences, their names for things) or **about the system** (what this team calls its accounts, which region matters, how it deploys) | The two things worth knowing |
| Memory is data, never instruction | Notes go into the prompt in a `<memory>` block that says they are notes written by people, they grant nothing and never override the rules. Nothing in memory can change a role, a policy or a ceiling, because those are checked in code, not in the prompt | Same rule as `<untrusted_context>` |
| Who can write a note | **The person**, by adding or editing their own notes in the console. **The assistant**, with a `remember` tool, only as described next | Poisoned memory is the new attack: text in a log must not become a standing instruction |
| The `remember` rule | A note about the person is saved at once **only if the turn is clean**: no context attached and no lookup has returned data yet. Otherwise it is saved as *pending* and the person confirms it. A note about the system is **always pending** until an administrator approves it | A clean turn means the words came from the person. Anything else may have come from an attacker |
| What counts as clean | The default overview (run ids, pipeline names, counts) is clean. A run, a pipeline or the infrastructure attached as context is not, because it carries log text, YAML or resource names that someone else wrote | Otherwise every note would wait, and the page would be a chore, yet text from a log must never become standing memory unasked |
| Refused text | Anything that looks like a secret, anything over 300 characters, anything with markup that looks like our own tags | Memory must not become a place to keep secrets or to break out of the block |
| Learning day by day | Notes are ranked by how well they match the question, how recently and how often they helped, and the best few (about 1.5 KB) go into the prompt. A note that is never used fades from the ranking and, after 180 days, is removed. People see the use count and can delete anything | Better with use, and visible |
| Seeing and controlling | Each person has a Memory page: their notes, pending ones to confirm, the system notes, "forget everything". Administrators approve system notes and can turn memory off | Visibility is the safeguard that makes learning acceptable |
| Audit | Notes about the system are audited with their text (`memory.system.*`). Notes about a person are audited without the text (`memory.user.*`), so the log does not hold what they said | The log shows that memory changed, not what was private |

## The tool

`remember {"text":"...", "about":"me|system"}`. A developer or above, only where a person is present. It is not offered
to agents, to MCP clients or in plain `/chat` calls. The reply to the model says whether the note was saved or is
waiting for confirmation, and the console shows a card, so the person sees what was remembered and can undo it.

## Not in this slice

- Letting the model extract notes by itself after every conversation. It costs a model call per turn and is the most exposed to poisoning; the `remember` tool and the manual page come first.
- Meaning-based search (embeddings). The ranking is word overlap plus recency and use, which is enough for short notes and needs nothing else. A later note can add an embedding index behind the same interface.
- Usage insights (which questions repeat, which lookups fail) and a periodic summary of the system. They would produce *pending* system notes through the same path.
- Memory for agents. An agent gets system notes only if an administrator decides to let it; today it gets none.

## Built, and what was checked

Checked: the rules above with Go tests through the real server, each removed in turn to confirm a test fails (30 removals; the survivors, such as the guard against writing into another person's conversation that the stream also enforces, got their own tests and were then caught); a browser run against a scripted model showing the "Remembered" card, a note reaching the next prompt, a note after a lookup held as "Keep this note?", the history list and reopening a conversation, and the Memory page. The run showed one defect the tests had not: a note's use count went up on every model round of a question rather than once, fixed with a test.

Not checked: a real model deciding when to call `remember` (it may call it too often or too rarely; the rules bound the harm, not the judgement), a real model's use of the notes, a large installation (conversations of one person are found by listing the kind and filtering, which is fine for hundreds, not for very many; an index by owner would be the next step), and phones.

## Tests to write

Go, through the real server: a conversation is saved with its answer and lookup names and no lookup result; a secret
in a question or answer is hidden; another person (and an administrator) gets 404 for it; deleting works; the
limits hold. `remember` saves in a clean turn, is pending after a lookup or with context attached, and is always
pending for the system; a pending note is not in the prompt until confirmed; a secret-looking note is refused;
agents, MCP and `/chat` cannot write or read history. Notes reach the prompt, ranked, inside the block, and a note
saying "ignore the policy" does not change what `policyGate` does. Only administrators approve system notes. Then
remove each rule and confirm a test fails.
