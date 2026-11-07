---
title: Console tour
parent: Guides
nav_order: 1
---

# Console tour

Everything below is done from the web console. Pictures were taken from a running server in a real browser, after the redesign, on stand-ins for AWS and GitHub with demo data. The agent-step and assistant pictures are older and still show the previous indigo colour; those pages work the same.

The sidebar holds the daily work, grouped by the job you are doing (design note 029): **Home** and **Approvals** at the top, then **Ship** (Pipelines, GitHub Actions), **Build** (Environments), **Team** (Agents, Marketplace) and **Watch** (Infrastructure, Kubernetes, Guard). Setup lives behind the **Settings** gear at the bottom: People and roles, Integrations, Secrets, Models, the Audit log and API tokens, grouped on one page with a menu on the left. Entries that are planned (single sign-on, access policy, budgets, approval rules) are listed dimmed as coming soon. What you see depends on your role, and old addresses still work.

## Home
**Needs you** lists what is waiting for you, most urgent first: changes you can approve (only ones someone else proposed, because a different administrator must approve), pipelines whose last run failed, open critical and high Guard findings, and connections that need attention. Changes you proposed are listed as waiting. Tiles above it count the same things, and leave out anything your role cannot read. Below are every pipeline with its status, a sparkline of its last runs, success rate and average time, and a strip with platform health.

The picture below is from before Home had the Needs you list.

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
Your AI team: agents, skills and workflows. Each agent has a face (its initials), says whether it is **On** or **Off**, and how much it may do: **Advises** (reads and explains, never asks for a change) or **Proposes** (can also draft cloud changes and ask for environments, which a person approves). Its page shows its instructions, the skills it follows, and **what it can use**, grouped (Pipelines and runs, AWS, Kubernetes, GitHub, Security and health…) and in plain words, with the technical name on hover; a shield marks a tool that asks a person to approve. You can talk to it beside its page.

**Edit** opens the editor in sections: who it is, how much it may do (two cards), instructions, skills it follows, and what it can use, each tool with a sentence about what it does. Instructions and skill steps are written in **Markdown** with a toolbar, a live preview and advisory checks (for example an agent that may only advise but is told to delete things, or text that looks like a credential). The checks never block a save. The model receives exactly the text you typed, and every save is a version you can roll back.

![An agent's page]({{ '/console/83-agent-page.png' | relative_url }})
![The agent editor]({{ '/console/82-agent-editor.png' | relative_url }})

## Integrations
One page for everything Forgeline connects to: GitHub, AWS, the AWS MCP Server, the Docker host, Kubernetes and Claude. Each card says what it is, whether it is **Connected**, **Needs attention** (with what to do and when it was last tested), or **Not connected**. AWS profiles already set up on the server are listed as **Found on this server**, by name only; Forgeline never reads their credentials. Systems that are planned (cloud model services, Lambda and other GPU clouds, Datadog and more) are shown dashed as **Coming soon, next** or **later**, with what each will do and how it will sign in. They have no Connect button because they cannot connect yet.

Click a card for its popup. It shows the status, who enforces read-only (the provider, or Forgeline alone), and, for administrators, a **Test connection** button. A test runs a few checks one at a time, each with a result, what it found and what to do if it failed. It only reads: it never creates or changes anything, and it says so. For administrators the popup also has an **Activity** tab: what was done to that integration and by whom, taken from the audit log, with refusals marked. **Open settings** goes to the existing settings for that system. Each popup has its own address, such as `#/integrations/aws`.

**The assistant panel.** It opens with what it can do (explain, prepare, propose) and a few things to try for the page you are on. While it works it shows each step as a sentence ("Listed AWS resources · primary · sqs"); click a step for the tool's name, its arguments and what came back. When it is done the steps fold into one line ("Looked at 3 things"). **New** starts a fresh conversation, the clock opens your past ones.

**Memory and history.** The assistant remembers short notes and keeps your conversations. Open **Settings → Memory** to see them: notes **about you** (private to you), notes **about this system** (shared, kept by an administrator), and your **conversations**. You can tell the assistant "remember that I work in eu-west-1", or write a note yourself; the assistant uses the notes that match what you ask as background, never as orders, so a note cannot widen what it may do. It saves a note by itself only when you said it in your own words. If it has just read logs or cloud data, or you attached a run or a pipeline, a note it wants to keep waits as **Keep this note?** until you press Keep, because that text may not have come from you; a note about the system always waits for an administrator. Passwords and keys are refused. Your conversations are private to you (administrators cannot open them), are kept up to 90 days and at most 100, never include what a lookup returned, and hide anything shaped like a key. Open an old one from the clock in the assistant panel; delete any or all from the panel or the Memory page. Notes nobody uses fade after 180 days. An administrator can turn memory off.

![The assistant keeping a note and using it next time]({{ '/console/80-assistant-remembers.png' | relative_url }})

![The Memory page]({{ '/console/81-memory-page.png' | relative_url }})

**Services the AI may use (AWS).** At the top of the AWS Access tab, each AWS service has a switch: File storage (S3), Containers (ECS), Image registry (ECR), Virtual machines and networks (EC2), Functions (Lambda), Databases (RDS), Tables (DynamoDB), Load balancers, Queues (SQS), Notifications (SNS), Stacks (CloudFormation), Logs and alarms (CloudWatch), Costs, and **Other services** for anything not listed. Everything starts on. A service an administrator turns off is left out of what the assistant, agents and MCP clients read (resources and Guard findings, with a line saying what was left out), refused in what they run or propose, and a proposal filed before it was turned off cannot be approved. People still see everything in the explorer: the switches govern the AI, not people. Each change is in the audit log.

**Access policy.** For the systems whose tools an AI can use (AWS, the AWS MCP Server, Kubernetes, Google Cloud and Azure) the popup has an **Access** tab. It lists each tool and lets an administrator set it to **Allow**, **Needs approval** or **Blocked**; nothing is restricted until someone does it. A blocked tool is refused for everyone, including administrators and agents that list it, it is not shown to the model or offered over MCP, and each refusal is recorded in that integration's Activity. **Needs approval** is for a read: when the assistant is about to run it, the assistant panel stops and shows a card with the call and **Approve** and **Deny**. Nothing runs until the person who asked approves; no answer in two minutes counts as a denial; an answer counts once and only from the person who asked. Where nobody is present (an agent run, an MCP client, a plain chat request) there is no one to ask, so the tool is refused there and is not offered. A tool that asks for a change is never Allow: it stays a proposal that a different administrator approves, though it can be blocked (and later put back). Below the list, a table shows what each agent can actually do: allowed, blocked by the connection, needs a person (so refused in an agent run), or not in the agent's own tool list. Developers can read it; only administrators change it. If the stored policy ever cannot be read, every tool of these integrations is refused until it is fixed, and the page says so.

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
Users and roles, and an append-only record of who did what, including refused actions and failed logins.

![Audit log]({{ '/console/26-audit-log.png' | relative_url }})

## Dark theme and phones
Both are supported.

![Dark]({{ '/console/28-overview-dark.png' | relative_url }})
