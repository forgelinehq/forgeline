---
title: Console tour
parent: Guides
nav_order: 1
---

# Console tour

Everything below is done from the web console. Pictures were taken from a running server in a real browser. Some older pictures still show the previous indigo colour and flat menu; the pages work the same.

The sidebar groups pages by the job you are doing: **Ship** (Pipelines, Actions, Environments), **Cloud** (Infrastructure, Kubernetes, Guard, Integrations), **AI** (Agents, Marketplace) and **Govern** (Proposals, Secrets, People, Audit log). What you see depends on your role.

## Overview
Every pipeline with its current status, a sparkline of its last runs, success rate and average time. Failing pipelines sort first. A strip at the top shows platform health.

![Overview]({{ '/console/07-overview.png' | relative_url }})

## Pipelines
A YAML editor with line numbers and validation by the server as you type, plus a live graph. **Add step** builds a step from a form (needs, network, secrets, time limit, or a Claude Code agent). Every save is a version you can view or restore.

![Editor]({{ '/console/04-editor-graph.png' | relative_url }})
![Step builder]({{ '/console/30-step-builder.png' | relative_url }})
![Versions]({{ '/console/31-version-history.png' | relative_url }})

## Runs
The steps as a graph coloured by status, with live logs. A failure selects the failing step, skips its dependents and shows the exit code and stderr.

![Failed run]({{ '/console/08-run-failed.png' | relative_url }})

## Secrets
Names, where each is stored and which pipelines use it. Values are write-only: nobody can read one back, and a step's output shows `***` where it printed one.

![Secrets]({{ '/console/10-secrets-list.png' | relative_url }})

## Actions
Start, watch and re-run GitHub Actions workflows of your allowed repositories. See [GitHub Actions]({{ '/guides/github-actions/' | relative_url }}).

![Actions]({{ '/console/34-actions-runs.png' | relative_url }})

## Infrastructure
Three tabs: **Platform** (the Forgeline server, Docker host and running steps), **AWS account** (the resources in the account) and **Cloud changes** (proposals and approvals). See [Connect AWS]({{ '/guides/connect-aws/' | relative_url }}).

The AWS tab opens on a **Map**. Each resource sits inside the VPC its own record names; the rest are grouped by kind (compute, storage, databases and so on). A strip at the top lists what **needs a look**: a failing or changing state, a security group open to the internet, an unattached or unencrypted volume, a database that is publicly accessible or not encrypted. Click any resource to see its detail beside the map. **List** is one click away, and the search box filters both.

![AWS map]({{ '/console/70-aws-map.png' | relative_url }})

{: .note }
The map shows only what AWS states about each resource. It draws no lines between resources, because the inventory does not say what talks to what, and a resource with no health signal is never shown as healthy. Logs, alarms and metrics are designed ([note 023](https://github.com/forgelinehq/forgeline/blob/main/docs/design/023-infrastructure-map-and-logs.md)) but not built.

![Cloud change]({{ '/console/23-deploy-preview.png' | relative_url }})

## Agents
Agents, skills and workflows. Instructions are written in **Markdown** with a small toolbar, a live preview and advisory checks (for example an agent that is only allowed to read but is told to delete things, or text that looks like a credential). The checks never block a save. The model receives exactly the text you typed, and every save is a version.

![Instructions editor]({{ '/console/71-agent-markdown-editor.png' | relative_url }})

## Integrations
GitHub (App or token), the assistant, and AWS. See [Connect GitHub]({{ '/guides/connect-github/' | relative_url }}) and [Assistant and agents]({{ '/guides/assistant-and-agents/' | relative_url }}).

## People and audit log
Users and roles, and an append-only record of who did what, including refused actions and failed logins.

![Audit log]({{ '/console/26-audit-log.png' | relative_url }})

## Dark theme and phones
Both are supported.

![Dark]({{ '/console/28-overview-dark.png' | relative_url }})
