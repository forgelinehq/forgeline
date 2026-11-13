---
title: Real checks, 2026-11-13
---

# MCP servers against real servers on the Pi, 2026-11-13

The check that [note 042](../design/042-mcp-servers.md) asks for: real public remote MCP servers and real local servers
in containers, on the maintainer's Raspberry Pi Forgeline (aarch64, Docker 29.9.2), after #119 deployed. Calls were made
by test agents using the real Claude model; administration used the server token. Everything was removed afterwards.

## Remote servers

| Server | Address | Spoke | Tools | Could be turned on |
| --- | --- | --- | --- | --- |
| Microsoft Learn | `https://learn.microsoft.com/api/mcp` | MCP 2026-07-28 | 3 | all 3 (marked read-only) |
| DeepWiki | `https://mcp.deepwiki.com/mcp` | MCP 2026-07-28 | 3 | none: its tools carry no `readOnlyHint`, so Forgeline refuses them |

A T0 agent with only `mcp_mslearn_microsoft_docs_search` answered two questions from the tool's results (about 20 KB
back in 1.3 s each). Turning on a DeepWiki tool was refused with the reason. Each call is in the audit log with the
server, tool, sizes and time.

## Local servers (containers)

| Image (pinned by digest) | Result |
| --- | --- |
| `mcp/time`, `mcp/fetch` (Docker's MCP catalog) | Did not start: `exec mcp-server-time failed: Permission denied`. These images keep their Python under `/root`, and Forgeline runs every container as user 65534 (nobody), which cannot reach it. |
| `ghcr.io/github/github-mcp-server` (static binary) | Started in the sandbox and answered over stdio: 46 tools. 19 read-only tools could be turned on; 19 tools that change things were refused; 8 read-only tools were refused because an argument is an array (`fields`), which Forgeline's argument checks do not cover yet. |

A T0 agent with `mcp_github_list_branches` (host list `api.github.com`, a dummy token from the vault) called it in a
container; the call ran out of time after 30 s and the egress proxy saw no request.

## Found and fixed (#120)

- **Audit actor.** Calls from an agent chat were audited as `system`. They now name the person who asked; re-checked
  on the Pi after #120 deployed.
- **The Pi's firewall blocks the egress proxy.** ufw (`INPUT DROP`) drops traffic from containers to the host, where the
  per-call proxy listens on the internal network's gateway. Shown by hand: a container on an `--internal` network cannot
  reach a listener on the gateway on the Pi, and can on a machine without ufw. Run directly with Docker on an open
  network, the same GitHub image answered in under a second (`401 Bad credentials` for the dummy token), and with a
  dead proxy set it hung, so it does use the proxy. After #120 the error says that no request reached the proxy and that
  a firewall may be blocking it. The Pi's firewall was not changed.

## Open

- **Plugins and local MCP servers with a host list cannot work on the Pi** until the firewall lets the Docker networks
  reach the host (the maintainer's decision). A fixed proxy port range would make a narrow rule possible.
- **Images that need root**, like `mcp/time` and `mcp/fetch`, do not run. Running as the image's own user would weaken
  the sandbox; it needs a decision.
- **Public servers without `readOnlyHint`**, like DeepWiki, cannot be used. An administrator marking a tool read-only
  themselves (audited, with the risk shown) is a possible next step.
- **Array arguments** are not checked yet, which keeps 8 GitHub tools off.

## Cleaned up

Five MCP servers removed; the two test agents disabled (agents cannot be deleted); the dummy secret deleted; the
pulled images (`mcp/time`, `mcp/fetch`, `github-mcp-server`, `busybox`) removed; no leftover containers or networks.
