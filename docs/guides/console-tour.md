---
title: Console tour
parent: Guides
nav_order: 1
---

# Console tour

Everything below is done from the web console. Pictures were taken from a running server in a real browser, after the redesign, on stand-ins for AWS and GitHub with demo data. The agent-step and assistant pictures are older and still show the previous indigo colour; those pages work the same.

The sidebar has five spaces, named after the job you are doing (design note 029): **Home** (what needs you), **Build** (what should exist), **Ship** (getting code out), **Team** (your AI agents) and **Watch** (how it is running). The pages live as tabs at the top of each space: Home has Today and Approvals; Build has Environments and Blueprints; Ship has Pipelines and GitHub Actions; Team has Team (your agents as an org chart), Agents and Marketplace; Watch has Map, Infrastructure, Kubernetes and Security. Tabs marked **soon** (Design, Missions) are planned and have no page yet. **Approvals** is also a button at the top right of every page, with the number of changes waiting. Setup lives behind the **Settings** gear at the bottom: People and roles, Sign-in (SSO), Connections, Secrets, Models, Memory, the Audit log and API tokens, grouped on one page with a menu on the left. Approval rules (which changes need two approvals, and who may approve) are under Security. Entries that are planned (budgets) are listed dimmed as coming soon. What you see depends on your role, and old addresses still work.

## Home
At the top, **Ask or tell Forgeline** opens the assistant with your question (developers and administrators). Below it a greeting says how much needs you, and four tiles count your pipelines, your AWS **spend** (loaded only when you press **Load from AWS**, because AWS bills each look), your **AI team** and **security** findings. **Start something** offers the next steps (build and test a GitHub repository with the assistant, write a pipeline, hire an agent) and **Recent changes** lists the last changes that were approved, rejected or applied.

**Needs you** lists what is waiting for you, most urgent first: changes you can approve (only ones someone else proposed, because a different administrator must approve), pipelines whose last run failed, open critical and high Guard findings, and connections that need attention. Changes you proposed are listed as waiting. Tiles above it count the same things, and leave out anything your role cannot read. Below are every pipeline with its status, a sparkline of its last runs, success rate and average time, and a strip with platform health.



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
**Watch › Map** shows the same map on its own page, with a switch between your AWS accounts. Resources a CloudFormation stack made sit inside that stack's dashed box, and the stack's state is explained in plain words when you pick it (for example, a rolled-back stack cannot be updated, only deleted and deployed again). A line joins a Lambda function to the queue or table stream it reads. Pick anything to see which stack made it, what it uses and what uses it. When Forgeline flags something, the button reads **Ask the assistant to fix it**: it fills in the question for you to send, and any fix is a proposal someone approves.

![Watch map]({{ '/console/89-watch-map.png' | relative_url }})

The map shows only what AWS states about each resource. Lines come only from what Lambda states, nothing is linked by similar names, and a resource with no health signal is never shown as healthy. The map needs two read permissions for stacks and lines (`cloudformation:ListStackResources`, `lambda:ListEventSourceMappings`, both in `ReadOnlyAccess`); without them it still works and says what is missing. Logs, alarms and metrics are designed ([note 023](https://github.com/forgelinehq/forgeline/blob/main/docs/design/023-infrastructure-map-and-logs.md)) but not built.

![Cloud change]({{ '/console/23-deploy-preview.png' | relative_url }})

## Blueprints

**Build › Blueprints** holds designs of what should exist in an AWS account (design note 035). Click **New blueprint**, name it, pick the account and region, and start from **Empty**, **Small web app** (a front door, one small app, a table and logs) or **Queue worker**. A blueprint is made of blocks (a front door, apps on ECS, tables, file buckets, queues, logs, and things that already exist, which are used but never changed) and the links between them, written as sentences such as "App reads and writes Data". **What it means** lists in plain words what is open to the internet, whether data is kept, how long logs are kept, and what deserves a look. **Code** shows the OpenTofu configuration it generates, which you can download.

The safe settings are not options: files are private and encrypted, tables keep their data if you remove them from the blueprint, an app only takes traffic from its front door, and only a front door is reachable from the internet. Nothing is built yet: editing on the design canvas, previewing what AWS would do and sending it for approval come next, and building will always need a different administrator's approval.

![A blueprint]({{ '/console/90-blueprint.png' | relative_url }})

## Agents
Your AI team: agents, skills and workflows. Each agent has a face (its initials), says whether it is **On** or **Off**, and how much it may do: **Advises** (reads and explains, never asks for a change) or **Proposes** (can also draft cloud changes and ask for environments, which a person approves). Its page shows its instructions, the skills it follows, and **what it can use**, grouped (Pipelines and runs, AWS, Kubernetes, GitHub, Security and health…) and in plain words, with the technical name on hover; a shield marks a tool that asks a person to approve. You can talk to it beside its page.

**Edit** opens the editor in sections: who it is, how much it may do (two cards), instructions, skills it follows, and what it can use, each tool with a sentence about what it does. Instructions and skill steps are written in **Markdown** with a toolbar, a live preview and advisory checks (for example an agent that may only advise but is told to delete things, or text that looks like a credential). The checks never block a save. The model receives exactly the text you typed, and every save is a version you can roll back.

![An agent's page]({{ '/console/83-agent-page.png' | relative_url }})
![The agent editor]({{ '/console/82-agent-editor.png' | relative_url }})

## Connections
**Settings → Connections** lists what Forgeline is connected to on the left: GitHub, AWS, the Docker host, Claude and the rest, each saying **Working** or **Needs attention**. AWS logins already set up on the server are listed under **Found on this server**, by name only; Forgeline never reads their credentials. Pick one and it opens on the right:

- **Overview**: what it is, who enforces read-only (the provider, or Forgeline alone), its details, and for administrators **Test connection**. A test runs a few checks one at a time, each with a result, what it found and what to do if it failed. It only reads: it never creates or changes anything, and it says so. **Open settings** goes to the full settings for that system.
- **Services** (AWS): what the AI may do with each service. See below.
- **What the AI may do**: the tool ceiling and what each agent can actually do (the access policy, below).
- **Activity** (administrators): what was done to that connection and by whom, from the audit log, with refusals marked.

**Add a connection** shows everything not connected yet, with filters and search: ready to connect, and **Coming soon** systems (cloud model services, GPU clouds, Datadog and more) with what each will do and how it will sign in. Pick one to see it and connect it; some open a short wizard. Each connection has its own address, such as `#/integrations/aws`, so a link opens it directly.

**The assistant panel.** It opens with what it can do (explain, prepare, propose) and a few things to try for the page you are on. While it works it shows each step as a sentence ("Listed AWS resources · primary · sqs"); click a step for the tool's name, its arguments and what came back. When it is done the steps fold into one line ("Looked at 3 things"). **New** starts a fresh conversation, the clock opens your past ones.

**Memory and history.** The assistant remembers short notes and keeps your conversations. Open **Settings → Memory** to see them: notes **about you** (private to you), notes **about this system** (shared, kept by an administrator), and your **conversations**. You can tell the assistant "remember that I work in eu-west-1", or write a note yourself; the assistant uses the notes that match what you ask as background, never as orders, so a note cannot widen what it may do. It saves a note by itself only when you said it in your own words. If it has just read logs or cloud data, or you attached a run or a pipeline, a note it wants to keep waits as **Keep this note?** until you press Keep, because that text may not have come from you; a note about the system always waits for an administrator. Passwords and keys are refused. Your conversations are private to you (administrators cannot open them), are kept up to 90 days and at most 100, never include what a lookup returned, and hide anything shaped like a key. Open an old one from the clock in the assistant panel; delete any or all from the panel or the Memory page. Notes nobody uses fade after 180 days. An administrator can turn memory off.

![The assistant keeping a note and using it next time]({{ '/console/80-assistant-remembers.png' | relative_url }})

![The Memory page]({{ '/console/81-memory-page.png' | relative_url }})

**Services (AWS).** Each AWS service has three levels for the AI: **Off** (it cannot see it), **Look only** (it can read it but never propose a change) and **Change, with approval** (it can propose changes; a different administrator approves each one). The services: File storage (S3), Containers (ECS), Image registry (ECR), Virtual machines and networks (EC2), Functions (Lambda), Databases (RDS), Tables (DynamoDB), Load balancers, Queues (SQS), Notifications (SNS), Stacks (CloudFormation), Logs and alarms (CloudWatch), Costs, and **Other services** for anything not listed. Everything starts at Change, with approval. A service that is off is left out of what the assistant, agents and MCP clients read (resources and Guard findings, with a line saying what was left out) and refused in what they run or propose. A Look only service can be read, but a change to it cannot be proposed. A proposal filed before a service was turned off or set to Look only cannot be approved. People still see everything in the explorer: the switches govern the AI, not people. Each change is in the audit log.

**Access policy.** For the systems whose tools an AI can use (AWS, the AWS MCP Server, Kubernetes, Google Cloud and Azure) the connection has a **What the AI may do** tab. It lists each tool and lets an administrator set it to **Allow**, **Needs approval** or **Blocked**; nothing is restricted until someone does it. A blocked tool is refused for everyone, including administrators and agents that list it, it is not shown to the model or offered over MCP, and each refusal is recorded in that integration's Activity. **Needs approval** is for a read: when the assistant is about to run it, the assistant panel stops and shows a card with the call and **Approve** and **Deny**. Nothing runs until the person who asked approves; no answer in two minutes counts as a denial; an answer counts once and only from the person who asked. Where nobody is present (an agent run, an MCP client, a plain chat request) there is no one to ask, so the tool is refused there and is not offered. A tool that asks for a change is never Allow: it stays a proposal that a different administrator approves, though it can be blocked (and later put back). Below the list, a table shows what each agent can actually do: allowed, blocked by the connection, needs a person (so refused in an agent run), or not in the agent's own tool list. Developers can read it; only administrators change it. If the stored policy ever cannot be read, every tool of these integrations is refused until it is fixed, and the page says so.

![The Access tab]({{ '/console/77-access-policy.png' | relative_url }})

![The assistant asking before it reads Google Cloud]({{ '/console/78-approval-card.png' | relative_url }})

The assistant can also read Google Cloud and Azure now (`gcp_resources` and `azure_resources`): the same inventory as the explorer tabs, as text, read-only, from the cache when it is fresh.

![The Access tab]({{ '/console/77-access-policy.png' | relative_url }})

**Google Cloud and Azure connect inside the popup.** Choose **Connect** and a three-step wizard asks what to read (a scope, or which subscriptions), has the server prove the login can read it before saving anything, and then runs the test. If the server's login is missing or lacks permission, the wizard says which role or command is needed and nothing is saved. Other systems still link to their own connect pages.

![The connect wizard, at its test step]({{ '/console/76-connect-wizard.png' | relative_url }})

![The integrations hub]({{ '/console/72-integrations-hub.png' | relative_url }})
![A test of the AWS connection]({{ '/console/73-integration-test.png' | relative_url }})

See [Connect GitHub]({{ '/guides/connect-github/' | relative_url }}), [Connect AWS]({{ '/guides/connect-aws/' | relative_url }}) and [Assistant and agents]({{ '/guides/assistant-and-agents/' | relative_url }}). The design, and what is coming, is in [note 025](https://github.com/forgelinehq/forgeline/blob/main/docs/design/025-integrations-hub-and-settings.md). Google Cloud and Azure have tabs under Infrastructure; see [Connect Google Cloud]({{ '/guides/connect-gcp/' | relative_url }}) and [Connect Azure]({{ '/guides/connect-azure/' | relative_url }}). Not built yet: wizards for the systems other than Google Cloud and Azure.

## People and audit log
Users and roles, and an append-only record of who did what, including refused actions and failed logins. Administrators can filter it by person, action and dates, download it as CSV or JSON Lines, and choose how long it is kept (forever by default).

![Audit log]({{ '/console/26-audit-log.png' | relative_url }})

## Dark theme and phones
Both are supported.

![Dark]({{ '/console/28-overview-dark.png' | relative_url }})
