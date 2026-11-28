---
title: Assistant sandbox
parent: Guides
nav_order: 32
---

# Assistant sandbox

When no lookup answers, the assistant can run commands and write and run code in a sandbox: a container Forgeline starts
for you. It is off until an administrator turns it on.

Use it for work no built-in lookup covers: parsing a file, building a report, trying a script, cloning and building a
project.

## At a glance

| | |
| --- | --- |
| Where in the console | **Settings → AI → Assistant permissions** (turn it on); the chat (use it) |
| Needs | Docker on the server |
| Design note | [052](../design/052-assistant-that-works-like-a-colleague.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| Turn the sandbox on, or choose no network | Administrator | Off until then |
| Use it in the chat | Developer | Only the person in the chat, who approves commands; agents and workflows cannot |
| Reset your sandbox (**Reset my sandbox**) | Developer | Removes your containers and your `/work` folder |
| Give a command a vault secret | Administrator | Sees the command and the secret's name on a card, answers once |
| Download a file from `/work` | Developer | Your own `/work` only |

## How it works

1. The assistant starts a container for you with root inside, the network on, and a `/work` folder that stays between
   calls.
2. It can `apt install`, `pip install`, `npm install`, `curl`, clone, build and run anything, in any language, and use any
   Docker image (`python:3.13`, `node:22`, `golang`).
3. The container is removed after 30 minutes unused. Its `/work` folder stays until you reset it.
4. A single command that only looks (`ls`, `cat`, `grep`, `head`, `wc` and the like) runs at once. Anything else shows an
   **Allow this command?** card: allow it once, for this chat, or deny it with a note
   ([how the cards work](assistant-agent-engines.md#answer-a-permission-card)). Autopilot runs it without a card.
5. These commands do not see `/work/.agent`, where an agent keeps its sign-in and sessions: they find it empty.

![The assistant ran code in its sandbox]({{ '/console/assistant-sandbox.png' | relative_url }})

## Turn it on

1. Open **Settings → AI → Assistant permissions**.
2. In the **Sandbox** card choose **Turn on**, and set **Network** to **Internet (can install things)** or **No network**. A new
   network choice applies to a new sandbox; choose **Reset my sandbox** to apply it to yours now.

## Get a file out of the sandbox

The sandbox has no way out of its own and no credentials, so a PDF, a CSV or a report it builds stays in `/work` until you
fetch it.

1. Ask for it: "build the PDF and give it to me".
2. The assistant checks that the file is there and puts a **Download** card in the chat. A file it changed while answering
   and names in the answer gets a card by itself, at most five per answer. Two files with the same name need the path.
3. Press it and the browser saves the file, fetched as you. The same list is at `GET /v1/assistant/files`.

Rules for downloads:

- Only **your own** `/work` folder, and only regular files in it: not a folder, not a link that points outside `/work`, and
  never `/work/.agent`, where an agent keeps its sign-in and sessions. The sandbox itself checks this when the file is
  read.
- At most **50 MB** (zip or split anything larger).
- It is always sent as a download, never opened in the console: a web page or SVG it made arrives as plain bytes, so it
  cannot run here.
- Each download is in the audit log, and so is an attempt to read outside `/work`.

## Security model

- It cannot see the server's files, its credentials or the Docker socket: nothing of the host is mounted, it is not
  privileged, and it has all capabilities dropped but a small set, `no-new-privileges`, 2 GB of memory, 2 CPUs and at most
  512 processes.
- It has **no credentials** unless you give it one. A command that needs a key names a vault secret; an administrator
  sees the command and the secret's name on a card and answers once. The value goes in as an environment variable, is
  hidden if the command prints it, and never reaches the model.
- It is **not offered to agents or workflows**, only to the person in the chat.
- A command that you let it run in the sandbox is audited with its text.

## Limits and verification

{: .warning }
**Know the limits.** A container shares the host's kernel unless the host runs gVisor (the page says which is in use).
Forgeline cannot close the container's network to other machines without firewall rules on the host: on this server's own
test, a service listening only on the loopback address was not reachable and the cloud metadata address did not answer,
but a service listening on all interfaces or on your network could be reached. If that matters, choose **No network**, or
point `FORGELINE_SANDBOX_NETWORK` at a Docker network you have fenced. Checked here with Docker 29.3 and a stand-in model;
gVisor and microVMs have not been tried.
On 2026-11-27 (main at `487f373`) the Sandbox card (already on there) and the download refusals (`/etc/passwd`, `..`, `/work/.agent`) were checked in the console and API as a developer; the off state, turning it on and running code were not. See the [note 2026-11-27](../testing/2026-11-27-uat-assistant-agents.md).

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| The assistant says it has no shell | The sandbox is off ("the sandbox is turned off"); an administrator turns it on. |
| A command with a secret is refused | Only an administrator can give a command a secret; the secret must exist in the vault (**Settings → Secrets**), and a command takes at most 8. |
| Files I made are gone | The container is removed after 30 minutes unused but `/work` stays; **reset** removes `/work` too. |
| A download is refused | The file is outside your `/work`, is under `/work/.agent`, is a folder or link, or is over 50 MB. |
| A workflow's agent cannot run code | The sandbox is not offered to agents or workflows. |

## Related

- [Assistant](assistant.md)
- [Assistant permissions and autopilot](assistant-permissions-and-autopilot.md)
- [Agent engines](assistant-agent-engines.md)
