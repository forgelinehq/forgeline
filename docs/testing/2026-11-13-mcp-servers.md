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

## Fixed and checked again (#122, #123)

With the maintainer's approval the Pi's firewall got one rule, `ufw allow from 172.16.0.0/12 to any port 47100:47199
proto tcp` (Docker networks only), and Forgeline got `FORGELINE_EGRESS_PORTS=47100-47199`. Then, with real servers and
the real model:

| Fix | Result |
| --- | --- |
| Egress through the firewall | GitHub's server reached `api.github.com` through the proxy (`allowed [api.github.com×1]`); the dummy token got `401 Bad credentials` in 1.4 s instead of a 30 s timeout. |
| Run as the image's own user | `mcp/time` started and answered (`2026-11-14T01:29:16+05:30` for Asia/Kolkata). |
| An administrator marks a tool read-only | DeepWiki's `read_wiki_structure`, marked and audited (`marked read-only by an administrator`), returned React's wiki outline. |
| List arguments | 26 of GitHub's 46 tools can now be turned on (19 before). The model asked `list_commits` for a field it does not offer; the refusal named the allowed values (#123), and its second call went through. |
| Stated writes | GitHub states `readOnlyHint: false` on its write tools. #122 offered them for marking; #123 refuses that (`its server says it changes things`), checked on the Pi. Tools turned on before the upgrade stayed on. |

Still open: `list_issues`, `issue_write` and `push_files` use JSON Schema beyond what Forgeline checks (objects inside
arguments).

## Cleaned up

Five MCP servers removed (and three more after the re-check); the three test agents disabled (agents cannot be deleted); the dummy secret deleted; the
pulled images (`mcp/time`, `mcp/fetch`, `github-mcp-server`, `busybox`) removed; no leftover containers or networks.
