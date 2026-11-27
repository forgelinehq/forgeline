---
title: Connections and AI access
parent: Guides
nav_order: 16
---

# Connections and AI access

**Settings → Connections** lists every system Forgeline is connected to: GitHub, AWS, the Docker host, Claude and the
rest. For each one you can test the connection, see its activity, and decide what the AI may do with it. People always
see what their role allows; the switches here govern the assistant, agents and AI tools.

Use this page to connect a system, to check that a connection still works, and to limit what AI can see or propose
before you let agents loose on an account.

## At a glance

| | |
| --- | --- |
| Where in the console | **Settings → Connections**, and **Settings → Setup** for the first-run list (Developer and above) |
| Needs | An administrator to connect or change anything |
| Design notes | [025 integrations hub](../design/025-integrations-hub-and-settings.md), [026 access policy](../design/026-access-policy.md), [030 service switches](../design/030-service-switches.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See connections and their access policy | Developer | |
| Test a connection, see its activity | Administrator | A test only reads and says so; each test is audited |
| Connect, change or disconnect a system | Administrator | |
| Set the AWS service switches | Administrator | |
| Set a tool to Allow, Needs approval or Blocked | Administrator | A tool that changes something can never be set to Allow |

## How it works

1. Open **Settings → Connections**. Each connection shows **Working** or **Needs attention**, with what to do. Logins
   found on the server (an AWS profile, for example) are listed by name only, as **Found, not connected**.
2. Pick one to see its tabs: **Overview** (with **Test connection**), **Services** (AWS only), **What the AI may do**
   and **Activity**. A developer sees the tabs but not **Activity**, and the test button is for administrators.
3. **Add a connection** lists everything not connected yet, including systems coming soon.
4. For setup details of each system see [Connect AWS](connect-aws.md), [Connect GitHub](connect-github.md),
   [Connect Google Cloud](connect-gcp.md) and [Connect Azure](connect-azure.md).

### Test connection

**Test connection** runs the connection's checks one at a time, each with a result, a time and a fix. It only reads. A
warning does not block connecting. The same checks run on a timer, so "needs attention" is current. The Claude test sends
one short question.

## Set what the AI may do with AWS

Under **Services**, set each AWS service to one of three levels.

| Level | What the AI can do |
| --- | --- |
| **Off** | It cannot see the service |
| **Look only** | It can read the service but never propose a change |
| **Change, with approval** | It can propose changes, and a different administrator approves each one |

The services are file storage (S3), containers (ECS), the image registry (ECR), virtual machines and networks (EC2),
functions (Lambda), databases (RDS), tables (DynamoDB), load balancers, queues (SQS), notifications (SNS), stacks
(CloudFormation), logs and alarms (CloudWatch), costs, and **Other services**.

![The AWS connection's Services tab, one switch per service]({{ '/console/connections-services.png' | relative_url }})

## Set what each tool may do

**What the AI may do** lists each tool of AWS, the AWS MCP Server, Kubernetes, Google Cloud and Azure. An administrator
sets each to one of:

| Setting | Meaning |
| --- | --- |
| **Allow** | The tool runs when asked |
| **Needs approval** | The assistant stops and asks the person who asked, with the exact call |
| **Blocked** | The tool is refused for the assistant and for agents |

A tool that changes something is never **Allow**: it stays a proposal a different administrator approves, though it can
be **Needs approval** or **Blocked**. Setting it to Allow is refused. A table shows what each agent can actually do, with the reason when it cannot.

## Security model

- **Read-only tests.** A connection test never creates, launches or changes anything.
- **Switches are enforced on the server.** An Off service is refused in the assistant's AWS calls, both proposal tools
  and the summary before anything reaches the CLI or the AWS MCP Server, for administrators and agents alike.
- **Changes are always proposals.** No setting lets the AI change your cloud directly.
- **Fail closed.** If the stored policy cannot be read, every tool is refused and approval is refused.
- **Credentials are not shown.** Found logins are listed by name only, and tokens are never returned.
- **Audited.** Every test, every policy change and each activity item is in the [audit log](audit-log.md); the
  **Activity** tab shows the newest 30 entries for that connection, refusals included.

## Limits and verification

{: .warning }
**Read through the console on 2026-11-27**, on the UAT instance (main at `487f373`, AWS, the AWS MCP Server, Claude, the Docker host and GitHub connected): the Connections page (five working connections, two server logins "Found, not connected", **Add a connection**), the AWS connection's **Overview** with the result of the last test (three checks, "created nothing, changed nothing"), its **Services** tab (one three-way switch per service) and its **What the AI may do** tab (the tools with their kind and Allow, Needs approval, Blocked). The test was run as an administrator on the server; it was recorded in **Activity** as `integration.test` with the actor, and an older entry `policy.refused` shows the switch refusing a proposal. A developer is refused the test, a policy change and a switch change with 403 "admin is required". The access policy was also tested through the real server with a stub for the command-line tools, and in a browser against stand-ins ([note 026](../design/026-access-policy.md)); the service switches with eleven rules each removed in turn to confirm a test fails ([note 030](../design/030-service-switches.md)). **Not checked.** Changing any switch or tool setting on the instance (the maintainer's settings), setting a changing tool to Allow, fail-closed behaviour, a real Google Cloud or Azure account beyond their read-only checks, the Claude test, and how the real AWS MCP Server behaves when a tool is blocked in the middle of a conversation.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| **Needs attention** | The test found a problem; open the connection to read what to do and when it last checked. |
| The assistant says it cannot see a service | Its switch is **Off**. |
| The assistant will read but never propose | The switch is **Look only**. |
| A tool cannot be set to Allow | It changes something, so it is always a proposal. |
| An agent is told it needs a person | The tool is set to **Needs approval** and an agent run has nobody to ask. |
| A tool is not offered to an agent | It is not in the agent's tool list, or it is **Blocked**. |

## Related

- [Connect AWS](connect-aws.md)
- [Console basics](console-basics.md)
- [Connect GitHub](connect-github.md), [Google Cloud](connect-gcp.md) and [Azure](connect-azure.md)
- [Infrastructure map and services](infrastructure-map-and-services.md)
- [Assistant](assistant.md)
- [People and roles](people-and-roles.md)
