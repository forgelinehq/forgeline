---
title: Home
nav_order: 1
permalink: /
---

# Forgeline

Forgeline is an open-source, self-hostable delivery platform where AI agents run safely. You
write pipelines as typed YAML, run them in Docker on your own machine or server, and set the whole
platform up from a web console: no flags, no curl.

{: .warning }
Pre-alpha. It is built to be tried on a laptop. Read [what is not checked yet]({{ '/status/' | relative_url }}) before pointing it at anything that matters.

![The overview page]({{ '/console/07-overview.png' | relative_url }})

## What you can do

| | |
| --- | --- |
| Run pipelines | Steps are containers. Independent steps run in parallel, unchanged steps come from a cache. |
| Use Claude Code as a step | An `agent:` step runs the `claude` CLI in a container, read-only by default, with your own login or key. |
| Ask the assistant | A panel in the console explains failed runs, reads your cloud and prepares pipelines and runs for you to confirm. It shows each step it takes, and never sees secret values. |
| Build an AI team | Agents with instructions, skills and a short list of tools, each **Advises** or **Proposes**. They work as the person who asks and can never do more than that person could. |
| Connect GitHub | Create a GitHub App from the console in one approval, or use a token. Pushes start runs; results come back as commit statuses. |
| Connect AWS | Sign in with AWS IAM Identity Center from the console, then view stacks and instances and propose changes. |
| Change the cloud safely | Every change is a proposal, a CloudFormation change set or the exact AWS calls the assistant drafted, that a **different** administrator approves. You choose which AWS services (S3, ECS, ECR…) the AI may use at all. |
| Keep secrets | Encrypted at rest, write-only, masked in logs, and only given to steps that ask. |

## Start here

1. [Get started]({{ '/get-started/' | relative_url }}): install, start the server, run your first pipeline (10 minutes).
2. [Console tour]({{ '/guides/console-tour/' | relative_url }}): every page, with pictures.
3. [Write pipelines]({{ '/guides/pipelines/' | relative_url }}), [connect GitHub]({{ '/guides/connect-github/' | relative_url }}), [connect AWS]({{ '/guides/connect-aws/' | relative_url }}).
4. [API reference]({{ '/api/' | relative_url }}) for scripts and CI.

Design notes (why it works the way it does) are in [`docs/design`](https://github.com/forgelinehq/forgeline/tree/main/docs/design).
