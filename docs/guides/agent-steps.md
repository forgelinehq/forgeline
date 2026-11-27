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
| Where | A `steps[].agent` block in a pipeline file; **Add step → Kind → Claude Code agent** in the pipeline editor |
| Needs | An agent image (`docker build -t forgeline-agent-claude deploy/agent`) and a credential in **Settings → Connections → Secrets** |
| Design note | [007](../design/007-agent-steps.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| Write and save a pipeline with an agent step | Developer | |
| Store the credential as a secret | Administrator | |
| Run it | Developer | Runs for a pull request never get the credential, so the step fails; a run for a push gets it only where an administrator allowed secrets for that repository |

## How it works

```yaml
steps:
  - id: review
    image: forgeline-agent-claude      # built from deploy/agent/Dockerfile
    agent:
      provider: claude-code
      prompt: Review the last commit and list risks.
      maxTurns: 5
      tools: [Read, Grep, Glob]        # the default; add Edit or Bash(go test:*) only if the step needs them
    permissions:
      network: egress
      secrets: [CLAUDE_CODE_OAUTH_TOKEN]
```

1. Create the credential with `claude setup-token` (or use `ANTHROPIC_API_KEY`) and store it under **Secrets**.
2. List it in the step's `permissions.secrets`, and leave the network on (`egress`, the default).
3. Forgeline runs `claude -p --output-format text --max-turns N --allowedTools=... -- <prompt>` in the container. The
   prompt is one argument after `--`, never passed through a shell, and your own `~/.claude` is never mounted.

### Fields

| Field | Meaning |
| --- | --- |
| `provider` | Required. Only `claude-code` exists. |
| `prompt` | Required. What to ask the agent. |
| `model` | Optional. Passed to the agent; empty uses its default. |
| `maxTurns` | 1 to 100; empty means 10. |
| `tools` | Tools the agent may use, such as `Read`, `Grep`, `Edit`, `Bash(go test:*)`. Empty means read-only: `Read`, `Grep`, `Glob`. A tool not listed is refused, because the agent cannot ask for approval in a pipeline. |

## Security model

- The step gets only the secrets and network its `permissions` declare.
- Forgeline never reads or refreshes your login; your own `~/.claude` is never mounted.
- Read-only by default: editing files or running commands must be listed in `tools`.
- The credential is withheld from runs GitHub started for a pull request (and for a push unless allowed), like any
  secret; see [Write pipelines](pipelines.md#secrets).

## Limits and verification

Check Anthropic's terms before using a subscription token (`setup-token`) for automation. An API key is the safer
production choice. Codex and Copilot agent steps are not built yet; Copilot can already answer as the
[assistant](assistant.md).

{: .warning }
**Run on the UAT instance on 2026-11-27** (main at `487f373`, a real `CLAUDE_CODE_OAUTH_TOKEN` in the vault, an agent image already on its Docker): a one-turn agent step answered `OK`; a step asking the agent to use `Bash` without listing it got "NOT ALLOWED" and the command did not run; a run listing `ANTHROPIC_API_KEY` (not stored) failed at once with the "is not configured" message; the guide's own image name failed with "pull access denied" because it was not built there; the validation messages for no credential, `network: none`, an unknown provider and `maxTurns: 101` match this page; a viewer cannot run a pipeline. The fields, defaults and command line were also compared with `internal/pipeline`. Design note 007 records a run with a real `setup-token` login on 2026-10-28.
**Not checked.** An agent step in a run GitHub started (the withheld credential), `Edit` and `Bash(...)` tools that are listed, `model`, and a build of `deploy/agent`.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| `steps[0].permissions.secrets: an agent step must request one of: CLAUDE_CODE_OAUTH_TOKEN, ANTHROPIC_API_KEY` | The step lists no credential. |
| `steps[0].permissions.network: an agent step needs network access to reach its provider` | The step sets `network: none`. |
| `steps[0].agent.provider: unknown provider ...; supported: claude-code` | Only `claude-code` is supported. |
| `steps[0].agent.maxTurns: must be between 1 and 100` | `maxTurns` is out of range. |
| The step fails because the agent refused a tool | The tool is not in `tools`. Add it, knowing the agent can then use it. |
| The step fails naming the secret, in a run for a pull request | Untrusted runs get no secrets. |
| The step fails with exit code 125 and "pull access denied" in its output | `forgeline-agent-claude` is not built on the server's Docker; build it from `deploy/agent`, or name an image the server has. The run page's "What went wrong" may wrongly blame the credentials for this (exit code 125 matches its credentials rule); read the end of the output. |
| The step fails with `secret "ANTHROPIC_API_KEY" is not configured; set FORGELINE_SECRET_ANTHROPIC_API_KEY ...` | The step lists a credential that is not stored under **Secrets** (or set in the server's environment). Store it, or list the one you have. |

## Related

- [Write pipelines](pipelines.md)
- [Assistant](assistant.md)
- [Agents](agents.md)
