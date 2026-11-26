---
title: Agent steps in pipelines
parent: Guides
nav_order: 39
---

# Agent steps in pipelines

An agent step is a pipeline step that runs the vendor's own `claude` CLI inside the step's container. The pipeline author
lists what the agent may use. Forgeline builds the command line and never reads or refreshes your login.

Use it when a pipeline needs an AI review or analysis as one of its steps. It is not the same as the
[assistant](assistant.md) (a panel you ask) or an [agent](agents.md) (a named, versioned definition used by teams and
workflows).

## At a glance

| | |
| --- | --- |
| Where | A `steps[].agent` block in a pipeline file; see [Pipelines](pipelines.md) |
| Needs | An agent image (built from `deploy/agent/Dockerfile`) and a credential in **Secrets** |
| Design note | [007](../design/007-agent-steps.md) |

## How it works

```yaml
steps:
  - id: review
    image: forgeline-agent-claude      # built from deploy/agent/Dockerfile
    agent:
      provider: claude-code
      prompt: Review the last commit and list risks.
      maxTurns: 5
    permissions:
      network: egress
      secrets: [CLAUDE_CODE_OAUTH_TOKEN]
```

1. Create the credential with `claude setup-token` (or use `ANTHROPIC_API_KEY`) and store it under **Secrets**.
2. List it in the step's `permissions.secrets`, and grant the network it needs.
3. Forgeline builds the command line and runs the `claude` CLI in the container. Your own `~/.claude` is never mounted.

Agent steps are read-only by default and run with whatever tools the pipeline author lists.

## Security model

- The step gets only the secrets and network its `permissions` declare.
- Forgeline never reads or refreshes your login; your own `~/.claude` is never mounted.

## Limits and verification

Check Anthropic's terms before using a subscription token (`setup-token`) for automation. An API key is the safer
production choice. Codex and Copilot agent steps are not built yet; Copilot can already answer as the
[assistant](assistant.md).

## Related

- [Pipelines](pipelines.md)
- [Assistant](assistant.md)
- [Agents](agents.md)
