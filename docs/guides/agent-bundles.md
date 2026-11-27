---
title: Agent bundles and registries
parent: Guides
nav_order: 38
---

# Agent bundles and registries

You can take an [agent](agents.md) out of Forgeline as a file, bring one in, and push one to a container registry so
anyone with access can run it with `docker agent run`.

Use it to move an agent between servers, to share one with people who do not run Forgeline, or to keep a copy under
version control. Neither file holds a key or a secret.

## At a glance

| | |
| --- | --- |
| Where in the console | **Download v<n>** and **Push v<n>** on an agent's page, **Import** on **Agents → Agents**; registries under **Settings → Agent registries**; pushes under **Home → Approvals → Agent pushes** |
| Needs | For a push, a registry and a token that may push |
| Design note | [046](../design/046-agent-export.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| Download (export) a version | Developer | Audited |
| Import a bundle | Administrator | Audited; a dry run shows the result first |
| Add, change, check or remove a registry | Administrator | At most 20 registries |
| Propose a push | Administrator | Needs a reason |
| Approve or reject a push | Administrator | Not the person who proposed it; [approval rules](people-and-security.md) also apply |
| Cancel a waiting push | Administrator | |
| See pushes | Developer | The 100 newest |

## How it works

### Export

**Download** on an agent's page takes the version you are looking at out of Forgeline:

| Format | Contents |
| --- | --- |
| **Forgeline bundle (.zip)** | `agent.yaml` (instructions, model, tier, tools, skills and the version's fingerprint; schema in `schema/agent.v1alpha1.schema.json`), each skill as an Agent Skills folder (`skills/<name>/SKILL.md`, which Claude Code, Copilot, Cursor and others read as they are), and a README |
| **Docker Agent file (.yaml)** | Runs with `docker agent run`. It keeps the agent's remote MCP servers (only the tools it has) and its skills inline. Forgeline's own tools, plugins and local MCP servers stay behind, since they need the server; the file and its readme list them |

Neither file holds a key or a secret: they name the environment variables to set (`ANTHROPIC_API_KEY`, or
`${env.GITHUB_TOKEN}` for an MCP server's sign-in).

### Import

1. Choose **Import** on the Agents page and pick the .zip (from this server or another one, at most 512 KiB).
2. The dialog shows what will happen before anything is saved:
   - a new agent, or a new version of the agent with the same name;
   - each skill found by its text, or created;
   - the tools left out, with why: an MCP server or plugin this server does not have, or a proposal tool on a T0 agent;
   - a model this server does not have becomes the assistant's model.
3. Confirm. Importing the same bundle twice changes nothing.

![Importing a bundle]({{ '/console/agent-bundles-import.png' | relative_url }})

### Push to a registry

A push is a proposal.

1. Add the registry first under **Settings → Agent registries**: GitHub Container Registry, Docker Hub or any OCI registry,
   with a user name and a token that may push (kept in the vault, never shown again). Choose **Check** to see that it signs
   in.
2. On an agent's page, choose **Push**. The drawer shows the exact address (`host/namespace/repository:v<n>`) and asks why.
3. A different administrator reads the exact Docker Agent file under **Home → Approvals → Agent pushes** and approves it.
4. Only then does Forgeline sign in and push. The page shows the digest the registry confirmed and the command to run it.

![An approved push]({{ '/console/agent-bundles-push.png' | relative_url }})

Push statuses are `proposed`, `pushing`, `pushed`, `failed`, `rejected`, `cancelled` and `expired`. A push waiting longer
than 24 hours expires, and one whose registry was removed or moved is refused. Approval rules can ask for two approvals for
pushes (kind **Pushing an agent to a registry**).

## Security model

- No key or secret is in an exported file.
- A push needs a different administrator, who approves the exact file by its hash.
- The registry token stays in the vault and is never shown again.
- Every download, import, proposal, approval and push is in the audit log.

## Limits and verification

{: .warning }
**Checked with the real Docker Agent CLI.** On 2026-11-19 (CLI v1.162.0) a file for every provider kind loads
([note 046](../design/046-agent-export.md)). Pushing to a registry is checked against a local registry when
`FORGELINE_TEST_REGISTRY` names one. ECR through a connected AWS account, and the bundle as a second artifact type, are not
built.
On 2026-11-27 (main at `487f373`) on the UAT instance a developer exported both formats and an administrator imported a renamed bundle (dry run, create, unchanged); registries and pushes were not run (no registry there). See the [note 2026-11-27](../testing/2026-11-27-uat-assistant-agents.md).

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| A tool is missing after import | It needs an MCP server or plugin this server does not have, or it is a proposal tool on a T0 agent. |
| The registry **Check** fails | Wrong host, user name or token, or the token cannot read the repository. |
| A push is refused | Its registry was removed or its host or namespace changed since it was proposed. |
| An approval is refused | The approver proposed it, the hash differs, or the approval rules need more approvals. |
| A push disappeared | It waited more than 24 hours and expired. |
| The pulled agent cannot start | The environment variables the file names are not set, or a tool that stayed behind is needed. |

## Related

- [Agents](agents.md)
- [People and security](people-and-security.md)
- [Evals](evals.md)
