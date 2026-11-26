---
title: Assistant memory
parent: Guides
nav_order: 34
---

# Assistant memory

The assistant keeps files: how an environment is laid out, what a name like "Dev01" means, a procedure that worked, how you
like things done. It looks at them at the start of related work and searches them. Agents and workflows can read the
organisation's shared notes, and only those.

Use it so the assistant does not need telling the same thing twice.

## At a glance

| | |
| --- | --- |
| Where in the console | **Settings → AI → Memory** (the **Memory is on** switch is for administrators) |
| Needs | Memory switched on (an administrator can turn it off). Limits: 8,000 characters a file, 100 files a person, 200 for the organisation |
| Design notes | [027](../design/027-assistant-memory-and-history.md), [052](../design/052-assistant-that-works-like-a-colleague.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| Read your files and the organisation's | Developer | |
| Write, edit or delete your own files under `/memories/me` | Developer | |
| Write or delete files under `/memories/org` | Administrator | |
| Confirm a pending file of your own | Developer | |
| Turn memory on or off | Administrator | |
| "Forget everything about me" | Developer | Removes your files too |

## How it works

1. The assistant saves files itself. `/memories/me/...` is only yours; `/memories/org/...` is shared, and only an
   administrator writes there.
2. A fact that stops being true is not overwritten. The assistant gives the old file a **valid until** date (or you set one
   when you edit the file on the Memory page), writes a new file for what is true now, and the old one stays as history:
   "Dev01 meant the Mumbai cluster until March".
3. After its last valid day a file is marked *No longer valid*, and it is left out of the assistant's notes and of its
   searches. Clear the date to bring it back.

![Files the assistant keeps, on the Memory page]({{ '/console/assistant-memory.png' | relative_url }})

### Keep a file: the card

After a task that taught it something, the assistant can offer it as a card titled **Keep /memories/me/...** (the file's path). The card shows the path,
its text and who will see it (only you under `/memories/me`, everyone under `/memories/org`, which only an administrator
can keep).

1. Nothing is saved until you press **Confirm**.
2. The server checks the file again then (at most 8,000 characters, no secrets, no markup, not over an existing file: "the
   file ... already exists"), and saves it as yours.
3. Under [autopilot](assistant-permissions-and-autopilot.md) a clean conversation keeps it at once. Once the conversation
   has read logs or cloud data you did not type, the card always waits for you, because what it read may be what is being
   saved.

### Manage files

**Settings → AI → Memory** lists every file with its text: edit, delete or keep any of yours. A file it writes after reading logs or
cloud data you did not type waits for you to keep it, and is not used until you do.

## Use the notes from agents

Give an agent the **Read the organisation's shared notes** tool (`org_memory`) and it can list and search the files under
`/memories/org` and read them.

- It is read only: it cannot write, rename or delete.
- It never sees anyone's private files under `/memories/me`, or notes still waiting for a person to keep them. A note past
  its valid-until date is marked as no longer valid.
- What it reads is data for the agent, never instructions.
- An agent without the tool cannot read memory at all (or your sandbox). The assistant puts what such an agent needs into
  its instructions or a skill.

## Security model

- Files are background for the assistant, never instructions: they cannot change what it may do.
- It never saves a secret (that is refused), and markup is refused too.
- A file written after untrusted data waits for a person to keep it.
- `/memories/org` needs an administrator to write.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| The assistant ignores a file | Its valid-until date has passed (*No longer valid*), or it is still waiting for you to keep it. |
| A save is refused | The file is over 8,000 characters, has a secret or markup in it, or would overwrite an existing file. |
| Memory does nothing | An administrator turned memory off (the console says "Memory is turned off"; adding a note answers 409). Your conversation history is still kept. |
| A file is refused for the organisation | "only an administrator writes to /memories/org": save it under `/memories/me` and ask an administrator to move it. The assistant also refuses `/memories/org` after it read logs or cloud data you did not type. |
| An agent cannot read a note | It lacks the `org_memory` tool, or the note is under `/memories/me`. |

## Related

- [Assistant](assistant.md)
- [Agents](agents.md)
- [People and security](people-and-security.md)
