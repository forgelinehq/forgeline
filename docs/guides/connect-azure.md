---
title: Connect Azure
parent: Guides
nav_order: 7
---

# Connect Azure

Read-only. Forgeline uses the `az` login of the user the server runs as and one **Azure Resource Graph** query over your subscriptions. It cannot create, change or delete anything in Azure: the only commands it runs are `az graph query` (one fixed query, with no text from a person in it), `az account list` and `az account show`.

## Before you connect

On the server, as the user Forgeline runs as:

1. Sign in: `az login --use-device-code` (a person approves on another device), or sign in with a service principal.
2. Make sure that login has the **Reader** role on the subscriptions.
3. Install the Resource Graph extension: `az extension add --name resource-graph`.

## Connect

Go to **Infrastructure → Azure**. An administrator sees the subscriptions the login can see; tick the ones Forgeline may read, or tick none to read every subscription the login can see (narrower is safer). Choose **Connect**. Before it saves anything, Forgeline proves the login can query, and says what to fix if not: no login, no permission, the extension missing, or an unknown subscription.

You can also find it under **Integrations**, where **Test connection** runs the checks again at any time. It only reads.

## What you see

An explorer of everything in the chosen subscriptions, grouped by kind: virtual machines, App Service and Functions, AKS, container registries, storage accounts, disks, SQL servers, Cosmos DB, virtual networks, network security groups, public IPs, load balancers, Service Bus and Event Hubs, Key Vaults (names only), and **Azure OpenAI, AI services, AI Search and AI Foundry workspaces** under an AI group. A resource type Forgeline does not know yet is listed under **Other resources**. Resource Graph returns 1,000 rows per query, so Forgeline reads in pages up to 3,000 resources and says so when it reaches that limit. At most 200 are shown per kind, and the count is reported.

![The Azure explorer]({{ '/console/75-azure-explorer.png' | relative_url }})

{: .note }
**Verified:** the explorer, the connection and the test against a stand-in `az` (`scripts/dev/fake-az`), including paging, the cap, permission, login, missing-extension and not-installed errors, that only the three read commands ever run, and that only valid subscription ids reach the command. **Not verified:** a real Azure account. The shape of the real `az graph query` output and whether the Resource Graph extension is built in to your `az` version are assumed from the documentation. **Not built yet:** the map view and Guard rules for Azure, assistant lookups, spend, changes (writes), and signing in from the console. Design: [note 025](https://github.com/forgelinehq/forgeline/blob/main/docs/design/025-integrations-hub-and-settings.md).
