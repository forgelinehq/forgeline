---
title: Console tour
parent: Guides
nav_order: 1
---

# Console tour

Everything below is done from the web console. Pictures were taken from a running server in a real browser, after the redesign, on stand-ins for AWS and GitHub with demo data. The agent-step and assistant pictures are older and still show the previous indigo colour; those pages work the same.

The sidebar holds the daily work, grouped by the job you are doing: **Ship** (Pipelines, Actions, Environments), **Cloud** (Infrastructure, Kubernetes, Guard), **AI** (Agents, Marketplace) and **Decide** (Approvals, the changes waiting for a different administrator). Setup lives behind the **Settings** gear at the bottom: People and roles, Integrations, Secrets, Models, the Audit log and API tokens, grouped on one page with a menu on the left. Entries that are planned (single sign-on, access policy, budgets, approval rules) are listed dimmed as coming soon. What you see depends on your role, and old addresses still work.

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

The AWS tab opens on a **Map**. Each resource sits inside the VPC its own record names; the rest are grouped by kind (compute, storage, databases and so on). A strip at the top lists what **needs a look**: a failing or changing state, a security group open to the internet, an unattached or unencrypted volume, a database that is publicly accessible or not encrypted, or a CloudWatch alarm that is in alarm. Click any resource to see its detail beside the map. **List** is one click away, and the search box filters both.

![AWS map]({{ '/console/70-aws-map.png' | relative_url }})

{: .note }
The map shows only what AWS states about each resource. It draws no lines between resources, because the inventory does not say what talks to what, and a resource with no health signal is never shown as healthy. Logs, alarms and metrics are designed ([note 023](https://github.com/forgelinehq/forgeline/blob/main/docs/design/023-infrastructure-map-and-logs.md)) but not built.

![Cloud change]({{ '/console/23-deploy-preview.png' | relative_url }})

## Agents
Agents, skills and workflows. Instructions are written in **Markdown** with a small toolbar, a live preview and advisory checks (for example an agent that is only allowed to read but is told to delete things, or text that looks like a credential). The checks never block a save. The model receives exactly the text you typed, and every save is a version.

![Instructions editor]({{ '/console/71-agent-markdown-editor.png' | relative_url }})

## Integrations
One page for everything Forgeline connects to: GitHub, AWS, the AWS MCP Server, the Docker host, Kubernetes and Claude. Each card says what it is, whether it is **Connected**, **Needs attention** (with what to do and when it was last tested), or **Not connected**. AWS profiles already set up on the server are listed as **Found on this server**, by name only; Forgeline never reads their credentials. Systems that are planned (cloud model services, Lambda and other GPU clouds, Datadog and more) are shown dashed as **Coming soon, next** or **later**, with what each will do and how it will sign in. They have no Connect button because they cannot connect yet.

Click a card for its popup. It shows the status, who enforces read-only (the provider, or Forgeline alone), and, for administrators, a **Test connection** button. A test runs a few checks one at a time, each with a result, what it found and what to do if it failed. It only reads: it never creates or changes anything, and it says so. For administrators the popup also has an **Activity** tab: what was done to that integration and by whom, taken from the audit log, with refusals marked. **Open settings** goes to the existing settings for that system. Each popup has its own address, such as `#/integrations/aws`.

**Google Cloud and Azure connect inside the popup.** Choose **Connect** and a three-step wizard asks what to read (a scope, or which subscriptions), has the server prove the login can read it before saving anything, and then runs the test. If the server's login is missing or lacks permission, the wizard says which role or command is needed and nothing is saved. Other systems still link to their own connect pages.

![The connect wizard, at its test step]({{ '/console/76-connect-wizard.png' | relative_url }})

![The integrations hub]({{ '/console/72-integrations-hub.png' | relative_url }})
![A test of the AWS connection]({{ '/console/73-integration-test.png' | relative_url }})

See [Connect GitHub]({{ '/guides/connect-github/' | relative_url }}), [Connect AWS]({{ '/guides/connect-aws/' | relative_url }}) and [Assistant and agents]({{ '/guides/assistant-and-agents/' | relative_url }}). The design, and what is coming, is in [note 025](https://github.com/forgelinehq/forgeline/blob/main/docs/design/025-integrations-hub-and-settings.md). Google Cloud and Azure have tabs under Infrastructure; see [Connect Google Cloud]({{ '/guides/connect-gcp/' | relative_url }}) and [Connect Azure]({{ '/guides/connect-azure/' | relative_url }}). Not built yet: a per-tool access policy, and wizards for the systems other than Google Cloud and Azure.

## People and audit log
Users and roles, and an append-only record of who did what, including refused actions and failed logins.

![Audit log]({{ '/console/26-audit-log.png' | relative_url }})

## Dark theme and phones
Both are supported.

![Dark]({{ '/console/28-overview-dark.png' | relative_url }})
