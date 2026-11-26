---
title: Connect Azure
parent: Guides
nav_order: 7
---

# Connect Azure

Forgeline can list the resources of your Azure subscriptions, read-only. It uses the `az` login of the user the server
runs as and one **Azure Resource Graph** query. It cannot create, change or delete anything in Azure.

Use it to see what runs in Azure next to your other infrastructure.

## At a glance

| | |
| --- | --- |
| Where in the console | **Settings → Connections → Add a connection → Azure → Connect** (a three-step wizard: Choose, Connect, Test), or **Operations → Infrastructure → Azure** |
| Needs | `az` on the server, signed in as the user Forgeline runs as, with the **Reader** role and the Resource Graph extension |
| Design | [Note 025](../design/025-integrations-hub-and-settings.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See the connection status | Developer | |
| See the resources (the **Azure** tab, the assistant's `azure_resources` lookup) | Viewer | |
| See the subscriptions the login can read, connect, change the subscriptions, turn off | Administrator | |

## How it works

1. You choose subscriptions. Before it saves anything, Forgeline proves the login can query.
2. The only commands it runs are `az graph query` (one fixed query, with no text from a person in it),
   `az account list` and `az account show`. Only valid subscription ids reach the command.
3. Resource Graph returns 1,000 rows per query, so Forgeline reads in pages up to 3,000 resources and says so when it
   reaches that limit. Results are cached for 60 seconds.

## Connect

On the server, as the user Forgeline runs as:

1. Sign in: `az login --use-device-code` (a person approves on another device), or sign in with a service principal.
2. Make sure that login has the **Reader** role on the subscriptions.
3. Install the Resource Graph extension: `az extension add --name resource-graph`.

Then in the console, under **Which subscriptions?**, an administrator sees the subscriptions the login can see. Tick the
ones Forgeline may read (at most 20), or tick none to read every subscription the login can see (narrower is safer), and
choose **Connect**. If none is visible yet, you can still connect and choose later. Others see "An administrator chooses
the subscriptions." If the check fails it says what to fix, for example:

- "there is no valid Azure login on the server; run `az login --use-device-code` ..."
- "the Resource Graph extension is missing from the az command; run `az extension add --name resource-graph` ..."
- "Azure denied the request: the login needs the Reader role on the subscriptions"
- "Azure could not find one of those subscriptions; check the ids and that the login can see them"
- "the az command is not installed on the server"

Once connected it is listed under **Settings → Connections**, where **Test connection** runs the checks again at any
time, and the **Azure** tab has **Test connection** and **Turn off** (administrators). It only reads.

## What you see

An explorer of everything in the chosen subscriptions, grouped by kind: virtual machines, App Service and Functions,
AKS, container registries, storage accounts, disks, SQL servers, Cosmos DB, virtual networks, network security groups,
public IPs, load balancers, Service Bus and Event Hubs, Key Vaults (names only), and **Azure OpenAI, AI services, AI
Search and AI Foundry workspaces** under an AI group. A resource type Forgeline does not know yet is listed under
**Other resources**. At most 200 are shown per kind, and the count is reported.

![The Azure explorer]({{ '/console/connect-azure.png' | relative_url }})

## Security model

- **Read-only.** Only the three commands above run. Nothing in Azure is created, changed or deleted.
- **A fixed query.** `az graph query` runs one fixed query with no text from a person in it.
- **Proven before saved.** A connection that cannot query is refused.
- **Narrow by choice.** Choosing subscriptions limits what Forgeline reads; at most 20 can be chosen.

## Limits and verification

{: .warning }
**Verified:** the explorer, the connection and the test against a stand-in `az` (`scripts/dev/fake-az`), including paging,
the cap, permission, login, missing-extension and not-installed errors, that only the three read commands ever run, and
that only valid subscription ids reach the command. **Not verified:** a real Azure account. The shape of the real
`az graph query` output and whether the Resource Graph extension is built in to your `az` version are assumed from the
documentation. **Not built yet, as the original notes say:** the map view and Guard rules for Azure, spend, changes (writes),
and signing in from the console. The assistant can look at the subscriptions (`azure_resources` in
`internal/server/console_assistant_tools.go`); that lookup was not re-run for this review.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| Connect says there is no login | `az login` was not run as the user Forgeline runs as. |
| Connect says there is no permission | The login lacks the **Reader** role on the subscriptions. |
| Connect says the extension is missing | Run `az extension add --name resource-graph`. |
| Connect says `... is not a subscription id; it looks like 00000000-0000-0000-0000-000000000000` | Only valid subscription ids are accepted. |
| Connect says "choose at most 20 subscriptions" | Choose fewer, or none to read every subscription the login can see. |
| Connect says "Azure could not find one of those subscriptions" | Check the ids and that the login can see them. |
| Connect says "the az command is not installed on the server" | Install `az` for the user the server runs as. |
| Fewer resources than you expect | Forgeline reads up to 3,000 resources and says when it reaches that limit; 200 per kind are shown. |

## Related

- [Connect Google Cloud](connect-gcp.md)
- [Connect AWS](connect-aws.md)
- [People and roles](people-and-roles.md)
- [API: Azure](../api/reference.md)
