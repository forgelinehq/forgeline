---
title: MCP servers
parent: Guides
nav_order: 35
---

# MCP servers

An administrator can add an MCP (Model Context Protocol) server so agents and the assistant can use its tools. A **remote**
one is reached by its HTTPS address; a **local** one runs as a container image pinned by digest, like a plugin.

Use it when an agent needs a lookup Forgeline does not have: documentation search, a ticket system, a knowledge base.
Every tool starts off, and a tool that changes things can never be turned on.

## At a glance

| | |
| --- | --- |
| Where in the console | **Settings → Connections → MCP servers** |
| Needs | For a remote server, its HTTPS address (and a token from Secrets if it needs one); for a local one, an image pinned by digest |
| Design note | [042](../design/042-mcp-servers.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See, add, change, remove a server | Administrator | |
| Read its tools, turn tools on | Administrator | |
| Use a turned-on tool in an agent | Developer | The tool must be in the agent's tools |
| Use it from the assistant | Developer | The server must be marked **Offer its turned-on tools to the assistant** |

## How it works

1. An administrator adds the server. A remote server takes a token or header value taken from Secrets (never shown
   again). A local one is a fresh container per call, with no network unless you grant it (all of it, or only named
   hosts) and secrets from the vault as environment variables.
2. **Read its tools** shows what it offers. Every tool starts off.
3. The administrator turns tools on:
   - A tool its server marks read-only can be turned on.
   - Many servers do not mark their tools at all. If you have checked that such a tool only reads, turning it on asks you
     to confirm, and the tool shows **marked by** you here and in the audit log.
   - A tool its server says changes things (`readOnlyHint: false`) or is destructive can never be turned on, because it
     would bypass approvals.
4. A turned-on tool is named `mcp_<server>_<tool>`. Agents get it only when you add it to their tools.
5. Arguments are checked against the tool's own schema before the call. Every call is in the audit log, and what the tool
   returns is shown to the model as untrusted data.

![MCP servers]({{ '/console/mcp-servers.png' | relative_url }})

### What turns a tool off

| Event | Result |
| --- | --- |
| The server later offers a different description or arguments | The tool turns itself off (and loses your mark) until you read it again |
| You change the server's address or image | All its tools turn off |

### Local servers

A local server runs as the unprivileged user nobody. Some images (Docker's `mcp/time` and `mcp/fetch`, for example) keep
their program where only root can reach it and fail with `Permission denied`; for those, tick **Run as the image's own
user**. The file system stays read-only, with no extra privileges and the same network.

### Private addresses

A remote server on a private or cloud-internal address is refused unless you tick **It is on a private network**.
Redirects are never followed.

## Security model

- Every tool starts off; a tool that writes or destroys can never be turned on.
- A tool's definition is pinned when it is turned on.
- Local servers run with a read-only file system, no extra privileges, and no network unless granted.
- Secrets come from the vault and are never shown again.
- Results are untrusted data for the model; every call is audited.
- The assistant gets a server's tools only if you mark the server for it, and only for developers and above.

## Limits and verification

{: .warning }
**Checked against real servers on 2026-11-13** ([test note](../testing/2026-11-13-mcp-servers.md)): two public remote
servers and real local servers in containers, with a real model, on the maintainer's Raspberry Pi. Servers that do not
mark their tools read-only (DeepWiki, for example) cannot have a tool turned on without your own check.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| A tool cannot be turned on | Its server says it changes things or is destructive, or it carries no read-only mark and you have not confirmed that it only reads. |
| A tool turned itself off | The server's description or arguments changed; read its tools again. |
| All tools are off after an edit | Changing a server's address or image turns all its tools off. |
| A local server fails with `Permission denied` | The image keeps its program where only root can reach it; tick **Run as the image's own user**. |
| A local server with a host list always runs out of time | See `FORGELINE_EGRESS_PORTS` in [Get started]({{ '/get-started/' | relative_url }}#server-settings). |
| A remote address is refused | It is on a private or cloud-internal address; tick **It is on a private network**. |
| The assistant does not offer the tool | The server is not marked **Offer its turned-on tools to the assistant**. |

## Related

- [Agents](agents.md)
- [Assistant](assistant.md)
- [People and security](people-and-security.md)
