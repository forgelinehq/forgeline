---
title: Connect Google Cloud
parent: Guides
nav_order: 6
---

# Connect Google Cloud

Read-only. Forgeline uses the `gcloud` login of the user the server runs as, and lists the resources in one **scope** (a project, a folder or an organization) with a single Cloud Asset Inventory call. It cannot create, change or delete anything in Google Cloud: the only commands it runs are `gcloud asset search-all-resources`, `gcloud auth list` and `gcloud config get-value project`.

## Before you connect

On the server, as the user Forgeline runs as:

1. Sign in: `gcloud auth login` (or `gcloud auth activate-service-account`).
2. Make sure that login has the **Cloud Asset Viewer** role (`roles/cloudasset.viewer`) on the scope you want, and permission to use the project's APIs.
3. Enable the Cloud Asset API: `gcloud services enable cloudasset.googleapis.com`.

## Connect

Go to **Integrations → Google Cloud → Connect** (a short wizard), or **Infrastructure → Google Cloud**, and enter the scope (a bare project id such as `my-project` works, or `projects/my-project`, `folders/1234567890`, `organizations/1234567890`) and choose **Connect**. If the server's `gcloud` has a default project, it is filled in for you; Forgeline reads only its name. Before it saves anything, Forgeline proves the login can read the scope, and says what to fix if not: no login, no permission, the API not enabled, or an unknown scope.

You can also find it under **Integrations**, where **Test connection** runs the checks again at any time. It only reads.

## What you see

An explorer of everything in the scope, grouped by kind: Compute Engine VMs, Cloud Run, GKE clusters, Cloud Storage buckets, disks, Cloud SQL, BigQuery datasets, VPC networks, firewall rules, Pub/Sub topics, Secret Manager secrets (names only) and **Vertex AI endpoints and models** under an AI group. A resource type Forgeline does not know yet is listed under **Other resources** with its type, so nothing is hidden. At most 200 resources per kind are shown, and the true count is reported.

![The Google Cloud explorer]({{ '/console/74-gcp-explorer.png' | relative_url }})

{: .note }
**Verified:** the explorer, the connection and the test against a stand-in `gcloud` (`scripts/dev/fake-gcloud`), including permission, login, missing-API and not-installed errors, and that only the three read commands ever run. **Not verified:** a real Google account. The exact shape of the real `gcloud asset search-all-resources` output is assumed from its documentation and handled defensively. **Not built yet:** the map view and Guard rules for Google Cloud, assistant lookups, spend, changes (writes), and signing in from the console or with workload identity federation. Design: [note 025](https://github.com/forgelinehq/forgeline/blob/main/docs/design/025-integrations-hub-and-settings.md).
