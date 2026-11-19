---
title: Console tour
parent: Guides
nav_order: 1
---

# Console tour

Everything below is done from the web console. The pictures were taken on 2026-11-08 from the current console, in a
real browser, on stand-ins for AWS, GitHub and Claude with demo data.

The sidebar has five spaces, named after the job you are doing: **Home** (what needs you), **Build** (what should
exist), **Ship** (getting code out), **Team** (your AI agents) and **Watch** (how it is running). Each space has tabs at
the top. Setup lives behind **Settings** at the bottom. **Approvals** is also a button at the top right of every page,
with the number of changes waiting. What you see depends on your role, and old addresses still work.

| Space | Tabs |
| --- | --- |
| Home | Today, Approvals |
| Build | Design, Environments, Blueprints, Built |
| Ship | Pipelines, GitHub Actions |
| Team | Teams, Agents, Workflows, Missions, Library |
| Watch | Map, Services, Infrastructure, Kubernetes, Security |
| Settings | Setup, People and roles, Sign-in (SSO), Connections, Secrets, MCP servers, Image builds, Models and providers, Assistant model, Memory, AI use and budgets, Audit log, Approval rules, Resource types, Policy packs, API tokens |

## Home

Until a new server is set up, **Set up Forgeline** at the top lists the first-run steps (your team, GitHub, a cloud
account, the assistant, what the AI may touch, a first pipeline or blueprint), each with a button to the page that does
it. A step ticks itself when it is done; an administrator can skip one. **Settings › Setup** shows the list any time.

**Ask or tell Forgeline** at the top opens the assistant with your question (developers and administrators). Below it a
greeting says how much needs you, and four tiles count your pipelines, your AWS **spend** (loaded only when you press
**Load from AWS**, because AWS bills each look), your **AI team** and **security** findings.

**Needs you** lists what is waiting for you, most urgent first: changes you can approve (only ones someone else proposed,
because a different administrator must approve), pipelines whose last run failed, open critical and high security
findings, and connections that need attention. **Start something** offers next steps (build and test a GitHub repository
with the assistant, write a pipeline, hire an agent), and **Recent changes** lists the last changes approved, rejected or
applied. A strip at the bottom shows the Docker host, the server and the disk.

![Home]({{ '/console/100-home.png' | relative_url }})

**Approvals** lists every change waiting for a decision: AI proposals (the exact AWS calls an agent drafted) and cloud
changes (stacks, blueprints, environments). Each shows who asked, why, the preview, and how many approvals the
[approval rules]({{ '/guides/people-and-security/' | relative_url }}) need. A change filed by a mission says which one.

![Approvals]({{ '/console/101-approvals.png' | relative_url }})

## Build

**Design** opens a blueprint on the canvas: what should exist in an AWS account, drawn as blocks (a front door, apps on
ECS, tables, file buckets, queues, logs, MySQL or Postgres databases, Redis caches, secrets, and things that already
exist, which are used but never changed). An app can call another app, connect to a database or a cache, read a secret,
and scale between a fewest and a most number of tasks; a front door can send paths such as `/api/*` to a second app. Add blocks from
the toolbar, drag them into place (arrow keys move the one in focus), and pick one to change it on the right. Under
**Links** you join blocks; only links that make sense are offered. Orange lines carry traffic from the internet, grey
lines are an app using data, dashed lines are logs, and dashed blocks already exist. Moving dots show which way things
flow: the internet into a front door, traffic to the app, data back from what it reads, logs out. They show what the
blueprint configures, not measured traffic. Every change is checked as you go,
and **Save** stays off until a problem is fixed.

![The design canvas]({{ '/console/91-design-canvas.png' | relative_url }})

**Preview changes** asks OpenTofu what building the saved blueprint would do in AWS. It only reads. Each block then says
whether it would be added, changed, replaced or removed. **Send for approval** files the preview as a cloud change; a
different administrator approves it, and exactly that preview is applied. Afterwards the canvas shows **Built**.

![A preview on the canvas]({{ '/console/92-design-preview.png' | relative_url }})

**Remove…** appears once a blueprint is built. It makes a preview too, approved by someone else and applied exactly.
**Keep the data** removes apps, front doors, queues, caches and logs and leaves tables, buckets, databases and secrets,
still protected. **Remove everything** takes two approvals when something is protected: first deletion protection is
turned off and the rest removed, then the data is deleted (a final snapshot of each database is kept unless you untick
it). Afterwards the canvas shows **Removed**, and the blueprint can be built again. See [design note 041](https://github.com/forgelinehq/forgeline/blob/main/docs/design/041-removing-what-a-blueprint-built.md).

An **Agent** block runs your agent on Amazon Bedrock AgentCore from its container image in ECR (built for arm64,
answering on port 8080). Link it to the tables, files, queues and secrets it may use: its role gets exactly those and
Bedrock models, and AWS's documented runtime permissions, nothing else. It is reached through AWS's API, never a public
port.

An app or an agent can name where its **Code** lives: a GitHub repository, a branch and a folder. **Suggest a
repository…** reads the Dockerfiles in the repositories on the GitHub allow list and lists the folders that fit the block,
with why; **Use** fills them in. The blueprint then
makes a private image repository for it and a role that only that branch can use, from GitHub Actions, to push images.
**Build from GitHub…** shows the workflow to add to the repository (its actions pinned to commit SHAs) and a Forgeline
pipeline that starts it. Forgeline never pushes to your repository, and running a new image is a blueprint change you
preview and send for approval. GitHub's OIDC provider must be set up in the AWS account once.

Once the blueprint is built, **Run a new image…** lists the images pushed to the block's repository, newest first, and
marks the one running now. **Use and preview** saves the blueprint with that image and starts the preview, so the next
step is Send for approval. Only images from the block's own repository are offered; any other image is set in the
block's fields as before.

![Run a new image]({{ '/console/139-design-run-new-image.png' | relative_url }})

![Build from GitHub]({{ '/console/121-design-github-build.png' | relative_url }})

An app can run on **Fargate Spot** (Where it runs): much cheaper, but AWS may stop a task with two minutes' warning and
it is started again, so it suits workers and anything that can restart. "One regular task, the rest on Spot" keeps one
task on regular Fargate. The checks say which apps use Spot.

**Templates** are reviewed, versioned solutions from Forgeline's library, added like any block (the dashed buttons at
the end of the Add bar). The first is **Lambda function**: a container image from ECR on AWS Lambda, run on a schedule,
by messages on a queue linked to it, or on demand, with its own log group and a role that reaches only what it is
linked to. Its settings form is drawn from the template's inputs, and the server checks every save against the same
rules; **Read its code** shows the OpenTofu module it builds. A plan keeps the template's code, so the approved apply
builds exactly what was previewed.

A **Step Functions workflow** runs the Lambda functions it is linked to, one after another in the order you link them,
passing each result to the next and retrying a step that fails. Start it from the AWS console or the API. Right after
it is first built, AWS can take up to a minute to let its new role start the functions, so a run started at once may be
refused; start it again a minute later.

An **EKS cluster with namespaces** is built in two approved steps: the first preview makes the cluster and its nodes;
once they exist, the next preview adds the namespaces, each with a quota and a network policy that admits traffic only
from the same namespace. It costs about $0.10 an hour plus the nodes.

A **Data pipeline** takes records that apps linked to it send ("sends to"; each app gets the stream's name as
`<BLOCK>_STREAM` and may only put records into it), optionally reshapes them with a linked Lambda function, and stores
them as compressed JSON in the one bucket it is linked to, by day. Name the record's fields and types under Columns
(`event=string`, `at=timestamp`), and query them with Athena in the pipeline's workgroup, from the table
`<database>.records`; no crawler runs. The data lives in the bucket block, so removing what the blueprint built keeps
it unless you choose to remove everything.

![A Lambda function template on the canvas]({{ '/console/140-template-block.png' | relative_url }})

The same dialog offers **Built by Forgeline** instead: a `forgeline.yaml` for the repository with a `build` step that
pushes to the block's image repository, and the push rule that allows it (an administrator clicks **Allow this push**
once the blueprint has been previewed, since the rule needs the AWS account). No GitHub Actions and no role for GitHub:
Forgeline's own AWS connection pushes, only for runs GitHub starts for a push to that branch; pull requests build
without pushing. See [Build an image]({{ '/guides/pipelines/' | relative_url }}#build-an-image).

![Built by Forgeline]({{ '/console/139-built-by-forgeline.png' | relative_url }})

![An agent on the canvas]({{ '/console/120-design-agent.png' | relative_url }})

![Removing what a blueprint built]({{ '/console/119-design-removal.png' | relative_url }})

The **Assistant** panel beside the canvas takes a change in your own words ("add a place to keep interview recordings")
and answers with what it would change, as sentences. **Use this** puts it on the canvas, unsaved. It cannot read your
account or do anything itself, and it will not touch a service an administrator turned off or set to look only for the AI.

![The assistant beside the canvas]({{ '/console/94-design-assistant.png' | relative_url }})

**Blueprints** lists them. **New blueprint** starts from **Empty**, **Small web app** or **Queue worker**. Each blueprint
says in plain words what it means (what is open to the internet, whether data is kept, how long logs are kept) and shows
the OpenTofu code it generates. The safe settings are not options: files are private and encrypted, tables and buckets
keep their data if you remove them, and only a front door is reachable from the internet. Databases are private,
encrypted and protected from deletion, and AWS creates their passwords and keeps them in Secrets Manager: an app gets
the password when it starts (as `DB_PASSWORD` for a database block called `db`), and Forgeline never sees it. A secret
block is created empty; you set its value in AWS, and only the apps linked to it can read it.

**Guardrails** check every preview before it can be sent for approval. A red line is a **deny**, for example a database
open to the internet, an unencrypted disk, an IAM user, or a policy that allows every action: Send for approval stays off
until the blueprint changes, and the attempt is audited. An amber line is a **warning**, for example a web port open to
the internet on a front door or deleting a table: it does not stop anything, and the approver reads it next to the
changes. An administrator can add more rules under Settings › Policy packs (Trivy, Checkov, or your own Conftest
rules); their findings appear in the same list, and the line under it says which rule sets checked the preview.

For anything the blocks and templates do not cover, a **Resource** block can make any AWS resource type an
administrator allowed under Settings › Resource types, with a form made from the provider's schema.

![A blueprint]({{ '/console/90-blueprint.png' | relative_url }})

**Built** lists everything Forgeline created and keeps in your cloud accounts: each blueprint that was built, drawn as
an interactive diagram (its blocks as on the canvas, with moving dots showing which way things flow), stacks deployed
through approved changes, and environments that are up. Pick a block to see the AWS resources it made, by name, id and
ARN (nothing else from the saved state leaves the server). **Check against AWS** looks each one up in the account and
marks it as still there or missing, so you notice something deleted outside Forgeline.

![Built by Forgeline]({{ '/console/116-built.png' | relative_url }})

**Environments** are temporary stacks from templates, with an owner, a time to live and a cost estimate, approved like
any cloud change and removed when their time is up.

## Ship

**Pipelines** lists every pipeline with its last run, a sparkline of recent runs, its success rate and average time.

![Pipelines]({{ '/console/102-pipelines.png' | relative_url }})

A pipeline opens on a YAML editor that the server checks as you type, beside a live graph of its steps. **Add step** builds
a step from a form (needs, network, secrets, time limit, or a Claude Code agent). Every save is a version you can view or
restore.

![A pipeline]({{ '/console/103-pipeline.png' | relative_url }})

A **run** shows its steps as a graph coloured by status, with live logs; dots move along the lines into a step while it
runs. When it fails, **What went wrong** names the
failing step and its exit code, shows the end of its output, and offers **Ask the assistant**.

![A failed run]({{ '/console/104-run-failed.png' | relative_url }})

**GitHub Actions** starts, watches and re-runs workflows of the repositories an administrator allowed. See
[GitHub Actions]({{ '/guides/github-actions/' | relative_url }}).

## Team

**Teams** shows your teams of agents as cards: a lead, its members, and the last goal it was given. Open one and give it
a goal: the lead splits the work, the members look into it at the same time with their own tools, and the lead writes
one answer; the run is drawn live, with each member's task and findings. Below the teams, every agent is listed with
what it is doing (working on a workflow, waiting for your OK, asleep until its next run, waiting to be asked, or off),
the teams it is in and what it used this month. See [Teams]({{ '/guides/assistant-and-agents/' | relative_url }}#teams).

**Agents** lists your agents, skills and workflows. An agent has instructions (Markdown, with a live preview and advisory
checks), skills it follows, and a short list of tools, shown in plain words. **Advises** means it reads and explains;
**Proposes** means it can also draft changes, which a person approves. An agent can ask its own **model** (a quick one
for routing, a stronger one for analysis); by default it uses the assistant's. Every save is a version you can roll back, and you
can talk to an agent beside its page. **Delete** archives an agent: it can be restored for 30 days from Agents ›
Archived, then it is deleted for good.

**Workflows** runs agents together; the tab lists them as cards with how each starts and its last run, every run of every
workflow under **Runs**, and deleted ones under **Archived** for 30 days. Draw one on the canvas (**New workflow**), as in
n8n: the canvas fills the page; a node's **+** (or **Add node**) opens a side panel to search your agents, the logic
nodes and the lookups, and wires the new node in after the one you came from. Drag from a node's port to another node to
wire them, pick a node to set it, drag the background to move, Ctrl + scroll to zoom, and **Settings** holds how the
workflow starts and what it may do.

- **Agent**: an agent does a task; it may also write named fields, such as `severity`.
- **Lookup**: one read-only lookup, without a model.
- **Decide**: takes one wire, either by a rule on what earlier nodes wrote or by an agent choosing.
- **Ask a person**: waits for an answer or an approval.
- **Merge**: waits for parallel branches.
- **End**: finishes the run.

Loops are allowed. Each node runs at most its limit (3 unless you say up to 10), and a run at most 50 steps. The grid
button lays the graph out left to right. A run is drawn on the same canvas: nodes turn green or red, the wires taken are bold,
and picking a node shows what went in and what came out each time it ran. A question waiting for you is answered at the
top of the run. Only an agent's own tools at the "Proposes" level can propose changes, and a different administrator
approves them. Workflows made of stages still run as before; **Edit on the canvas** turns one into a graph as a new
version. A graph can also start from a signed webhook (**Webhook secret** on its page shows the secret once) or after a
pipeline run fails; agents read what started it under `trigger`.

![Agents]({{ '/console/106-agents.png' | relative_url }})
![An agent's page]({{ '/console/83-agent-page.png' | relative_url }})

**Missions** give one agent a goal and one AWS account. A mission goes through five stages, drawn live: discover (it only
reads), propose, your OK, verify (it looks again after a change) and report. Every finding names the lookup that showed
it; a claim that names no lookup the agent ran is left out and counted.

![A finished mission]({{ '/console/97-mission-done.png' | relative_url }})

**Library** offers ready-made agents, skills, teams, tools, guardrail packs and environment templates, installed as
versions you can review, and lists the reviewed OpenTofu templates the canvas offers. Filter by kind at the top. A
guardrail pack is installed turned off, and a tool (an MCP server) with no token and none of its tools on, so nothing
changes until an administrator turns it on. A team package installs its agents, a workflow and a team (a lead and members) in one
go:

| Package | What it gives you |
| --- | --- |
| Incident triage team | A run analyst and a platform checker look at the same time; a lead writes one report with the next steps |
| Weekly platform review | A security checker and a spend checker side by side; a lead writes the weekly review |
| AgentCore readiness | An advisor that checks an agent against Amazon Bedrock AgentCore's documented rules, region and permissions |
| Repository to pipeline | Reads an allowed GitHub repository and prepares a pipeline for you to confirm |
| Architecture review | A light Well-Architected review of an AWS account, citing AWS's documentation |
| Incident response | Runbooks for the first minutes, a rollback, the handover and the postmortem, and a scribe that keeps the timeline |
| Drift watch | Compares what each blueprint built with what is in AWS now |
| Upgrade planner | Runtimes and versions near the end of support, with dates from AWS documentation |
| Known misconfigurations, Cost guard, Data protection | Guardrail packs (Trivy, and Conftest rules) |
| GitHub (read only), AWS Knowledge, Terraform Registry | Tools agents can be given |
| Cost watch, Security review, Failed-run triage, Cluster health | Single agents for those jobs |

Every agent only reads unless its package says it may prepare something for you to confirm; nothing is saved, run or
changed without a person.

## Watch

**Map** shows your AWS account: resources inside the CloudFormation stack that made them, then the VPC they are in, then
the rest by kind. A line joins a Lambda function to the queue or table stream it reads, with moving dots when Lambda reports the link as
enabled (in the direction data goes; traffic itself is not measured); nothing is linked by similar names. A strip lists what **needs a look** (a failing state, a security group open to the internet, an unencrypted volume,
an alarm in alarm). Pick anything to see which stack made it, what it uses and what uses it, and, when something is
flagged, **Ask the assistant to fix it**. A switch at the top changes account.

![Watch › Map]({{ '/console/89-watch-map.png' | relative_url }})

**Services** shows one tile per AWS service (EC2, Lambda, S3, RDS, queues, stacks and so on). The page draws at once
and each tile is read on its own, a few at a time, so the first ones fill in while the rest are still being read; a
service the role may not list says so on its tile. Opening a tile shows everything of that kind in the account on its
own page, with search, details and alarms, read with that service's calls only. The Map's list view links to the same
pages ("Open its page").

![Watch › Services]({{ '/console/118-watch-services.png' | relative_url }})

**Infrastructure** has tabs for the **Platform** (the Forgeline server, the Docker host and running steps), each cloud
(**AWS account**, **Google Cloud**, **Azure**) and **Cloud changes** (stacks, blueprints and environments proposed, approved
and applied). **Kubernetes** lists clusters and what runs in them; **Security** lists the findings of Forgeline's checks.

![Infrastructure]({{ '/console/107-infrastructure.png' | relative_url }})
![A blueprint's plan waiting for approval]({{ '/console/93-blueprint-approval.png' | relative_url }})

## Settings

**Connections** lists what Forgeline is connected to (GitHub, AWS, the Docker host, Claude and the rest), each **Working**
or **Needs attention**, with logins found on the server listed by name only. Pick one to see its **Overview** (and
**Test connection**, which only reads and says so), its **Services** (AWS), **What the AI may do**, and its **Activity**.
**Add a connection** shows everything not connected yet, including systems coming soon.

![Connections]({{ '/console/108-connections.png' | relative_url }})

**Services (AWS)** sets, per service, what the AI may do: **Off** (it cannot see it), **Look only** (it can read it but
never propose a change) or **Change, with approval**. The services are file storage (S3), containers (ECS), the image
registry (ECR), virtual machines and networks (EC2), functions (Lambda), databases (RDS), tables (DynamoDB), load
balancers, queues (SQS), notifications (SNS), stacks (CloudFormation), logs and alarms (CloudWatch), costs, and **Other
services**. People still see everything; the switches govern the AI.

![The AWS connection's services]({{ '/console/115-aws-services.png' | relative_url }})

**What the AI may do** lists each tool of AWS, the AWS MCP Server, Kubernetes, Google Cloud and Azure, which an
administrator sets to **Allow**, **Needs approval** (the assistant stops and asks the person who asked, with the exact
call) or **Blocked**. A tool that changes something is never Allow: it stays a proposal a different administrator approves.
A table shows what each agent can actually do.

**Secrets** are write-only: nobody can read one back, and a step's output shows `***` where it printed one.

![Secrets]({{ '/console/113-secrets.png' | relative_url }})

**Memory** shows what the assistant remembers: notes **about you** (private), notes **about this system** (shared, kept by
an administrator) and your **conversations** (private to you, kept up to 90 days). The assistant uses notes as background,
never as orders; a note it took from logs or cloud data waits for you to press Keep; passwords and keys are refused.

![Memory]({{ '/console/112-memory.png' | relative_url }})

**Models and providers** connects model providers (Anthropic, OpenAI, Gemini, Azure OpenAI, AWS Bedrock, and
OpenAI-compatible servers such as Ollama), turns their models on with prices, a monthly limit and a fallback, and shows a
live log of every model call. See [Models and providers]({{ '/guides/models-and-providers/' | relative_url }}).

**AI use and budgets** shows what each agent used this month (tokens, model time, calls, and the cost where the claude
program reports it). An administrator can set a monthly budget per agent; at 80% the console warns, and when it is used
up the agent stops before its next model call.

![AI use and budgets]({{ '/console/95-ai-use.png' | relative_url }})

**People and roles**, **Sign-in (SSO)**, **Approval rules**, **Policy packs** and the **Audit log** are described in
[People and security]({{ '/guides/people-and-security/' | relative_url }}). The audit log records who did what, including
refusals and failed sign-ins; administrators can filter it, download it as CSV or JSON Lines, and choose how long it is
kept.

![Audit log]({{ '/console/109-audit.png' | relative_url }})

## Dark theme and phones

The moon button at the top switches to the dark theme, and every page works on a phone.

![Home in the dark theme]({{ '/console/110-home-dark.png' | relative_url }})
![Team on a phone]({{ '/console/111-phone.png' | relative_url }})
