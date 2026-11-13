# 042: MCP servers you add: remote and local

Status: **decided 2026-11-12; slice 1 (remote servers, local container servers, discovered tools, grants) built 2026-11-13.** Checked on the Pi against real remote servers and real containers; findings and open points in [the 2026-11-13 checks](../testing/2026-11-13-mcp-servers.md).
Extends note 018 (plugins: MCP servers shipped as container images in Marketplace packages) and note 020 (Forgeline as
an MCP server).

## Question

Teams already run MCP servers (connections that give an AI tools): hosted ones reached over HTTPS (GitHub's, Atlassian's,
their own) and local ones that run as a program. Today Forgeline only runs MCP servers that come inside a reviewed
Marketplace package, as a container image with its tools declared in the package. How does an administrator add any MCP
server, remote or local, without giving agents a way around Forgeline's rules?

## Decisions

1. **An administrator adds a server** in Settings › Connections › MCP servers. Two kinds:
   - **Remote**: an HTTPS URL speaking MCP's streamable HTTP transport. Authentication is none, or a bearer token or a
     named header whose value comes from the vault (write-only, never shown again). OAuth sign-in comes later.
   - **Local**: a container image, run the way plugins are (note 018): one fresh container per call, MCP over standard
     input and output, no network unless granted (all of it, or only listed hosts through Forgeline's proxy), secrets
     from the vault as environment variables, memory, CPU and time limits. A local program outside a container is not
     offered: principle 3, no in-process plugins.
2. **Tools are discovered, then approved.** Forgeline asks the server for its tools (`initialize`, `tools/list`). Every
   tool starts **off**; an administrator turns on each one. A tool the server marks read-only (`readOnlyHint`) can be
   turned on; a tool that is not marked read-only stays off in this slice and says why: anything it changes would bypass
   the proposal-and-approval path (principle 5). Changing tools through approval comes in a later slice.
3. **Pinned definitions.** When a tool is turned on, Forgeline keeps a hash of its name, description and input schema.
   If the server later offers a different definition (the "rug pull" where a trusted tool quietly changes), that tool is
   treated as off until an administrator looks at the change and turns it on again. Tools that disappear are dropped.
4. **Who may use them.** An enabled tool becomes a lookup named `mcp_<server>_<tool>`. Agents get it only when an
   administrator adds it to the agent's tools (like every other tool); the assistant gets it when the server is marked
   "for the assistant". Every call runs as the person or workflow that asked, is audited (server, tool, argument size,
   answer size, duration), and is limited in time and size. What a tool returns is wrapped as untrusted data, never
   instructions, like every lookup.
5. **Remote addresses are checked.** HTTPS only (plain HTTP only for loopback during development); no redirects followed;
   the name is resolved and the connection is made to a checked address that is not private, loopback, link-local or a
   cloud metadata address, unless an administrator ticks "private network" for a self-hosted server, which is shown on
   the server's card. This blocks using an MCP URL to reach the server's own network (SSRF).
6. **Arguments are checked against the tool's own input schema** before a call, with the same small JSON Schema subset
   plugins use; a tool whose schema uses more than that subset cannot be turned on.

## After the real check (2026-11-13)

The [check on the Pi](../testing/2026-11-13-mcp-servers.md) found three things that kept real servers out; each is now
an administrator's explicit, visible choice:

- **Tools without `readOnlyHint`.** Many servers (DeepWiki, for one) do not annotate their tools. An administrator who
  has checked a tool can mark it read-only when turning it on: the mark names them, is audited, is pinned with the
  definition and is dropped when it changes. Only a tool its server says nothing about can be marked: `readOnlyHint:
  false` (the server says it writes) and `destructiveHint` keep a tool off, and no mark overrides them.
- **Images that need their own user.** Some images keep their interpreter under `/root`, which nobody (65534) cannot
  reach. "Run as the image's own user" drops only `--user`; the read-only root, dropped capabilities,
  `no-new-privileges`, limits and network rules stay.
- **Host firewalls.** ufw drops traffic from containers to the host, where the egress proxy listens.
  `FORGELINE_EGRESS_PORTS` fixes the proxy's port range so one narrow rule (Docker networks to those ports) is enough.

Lists of simple values are now accepted as arguments, each item checked.

## Not in this slice

- Tools that change things (through an approval card), OAuth sign-in for remote servers, MCP resources and prompts,
  servers reached through a stdio program on the host, sharing a server's session between calls.

## Checks

- Tests: a stand-in remote MCP server (JSON and event-stream answers, session ids), discovery, turning tools on, a refused
  write tool, a changed definition turning a tool off, the private-address check, redirects refused, the vault header
  sent and never shown, calls audited, answers wrapped as untrusted data. Security checks are mutation-tested.
- On the Pi: a real public remote MCP server, and a local one in a container.
