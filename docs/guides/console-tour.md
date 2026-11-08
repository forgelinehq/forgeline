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
| Team | Team, Agents, Missions, Marketplace |
| Watch | Map, Infrastructure, Kubernetes, Security |
| Settings | People and roles, Sign-in (SSO), Connections, Secrets, Models, Memory, AI use and budgets, Audit log, Approval rules, API tokens |

## Home

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
ECS, tables, file buckets, queues, logs, and things that already exist, which are used but never changed). Add blocks from
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

The **Assistant** panel beside the canvas takes a change in your own words ("add a place to keep interview recordings")
and answers with what it would change, as sentences. **Use this** puts it on the canvas, unsaved. It cannot read your
account or do anything itself, and it will not touch a service an administrator turned off or set to look only for the AI.

![The assistant beside the canvas]({{ '/console/94-design-assistant.png' | relative_url }})

**Blueprints** lists them. **New blueprint** starts from **Empty**, **Small web app** or **Queue worker**. Each blueprint
says in plain words what it means (what is open to the internet, whether data is kept, how long logs are kept) and shows
the OpenTofu code it generates. The safe settings are not options: files are private and encrypted, tables and buckets
keep their data if you remove them, and only a front door is reachable from the internet.

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

**Team** draws your agents as an org chart: you at the top, then the agents that report to you. Each card says what the
agent is doing in words (working on a workflow, waiting for your OK, asleep until its next run, waiting to be asked, or
off), whether it **advises** or **proposes**, and its budget for the month. The mission in progress is drawn below.

![Your AI team]({{ '/console/105-team.png' | relative_url }})

**Agents** lists your agents, skills and workflows. An agent has instructions (Markdown, with a live preview and advisory
checks), skills it follows, and a short list of tools, shown in plain words. **Advises** means it reads and explains;
**Proposes** means it can also draft changes, which a person approves. Every save is a version you can roll back, and you
can talk to an agent beside its page.

![Agents]({{ '/console/106-agents.png' | relative_url }})
![An agent's page]({{ '/console/83-agent-page.png' | relative_url }})

**Missions** give one agent a goal and one AWS account. A mission goes through five stages, drawn live: discover (it only
reads), propose, your OK, verify (it looks again after a change) and report. Every finding names the lookup that showed
it; a claim that names no lookup the agent ran is left out and counted.

![A finished mission]({{ '/console/97-mission-done.png' | relative_url }})

**Marketplace** offers ready-made agents, skills and environment templates, installed as versions you can review.

## Watch

**Map** shows your AWS account: resources inside the CloudFormation stack that made them, then the VPC they are in, then
the rest by kind. A line joins a Lambda function to the queue or table stream it reads, with moving dots when Lambda reports the link as
enabled (in the direction data goes; traffic itself is not measured); nothing is linked by similar names. A strip lists what **needs a look** (a failing state, a security group open to the internet, an unencrypted volume,
an alarm in alarm). Pick anything to see which stack made it, what it uses and what uses it, and, when something is
flagged, **Ask the assistant to fix it**. A switch at the top changes account.

![Watch › Map]({{ '/console/89-watch-map.png' | relative_url }})

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

**AI use and budgets** shows what each agent used this month (tokens, model time, calls, and the cost where the claude
program reports it). An administrator can set a monthly budget per agent; at 80% the console warns, and when it is used
up the agent stops before its next model call.

![AI use and budgets]({{ '/console/95-ai-use.png' | relative_url }})

**People and roles**, **Sign-in (SSO)**, **Approval rules** and the **Audit log** are described in
[People and security]({{ '/guides/people-and-security/' | relative_url }}). The audit log records who did what, including
refusals and failed sign-ins; administrators can filter it, download it as CSV or JSON Lines, and choose how long it is
kept.

![Audit log]({{ '/console/109-audit.png' | relative_url }})

## Dark theme and phones

The moon button at the top switches to the dark theme, and every page works on a phone.

![Home in the dark theme]({{ '/console/110-home-dark.png' | relative_url }})
![Team on a phone]({{ '/console/111-phone.png' | relative_url }})
