---
title: Connect Google Cloud
parent: Guides
nav_order: 6
---

# Connect Google Cloud

Forgeline can list the resources of a Google Cloud project, folder or organization, read-only. It uses the `gcloud`
login of the user the server runs as, and reads one **scope** with a single Cloud Asset Inventory call. It cannot
create, change or delete anything in Google Cloud.

Use it to see what runs in Google Cloud next to your other infrastructure.

## At a glance

| | |
| --- | --- |
| Where in the console | **Settings → Connections → Add a connection → Google Cloud → Connect**, or **Operations → Infrastructure → Google Cloud** |
| Needs | `gcloud` on the server, signed in as the user Forgeline runs as, with the **Cloud Asset Viewer** role and the Cloud Asset API enabled |
| Design | [Note 025](../design/025-integrations-hub-and-settings.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See the connection status | Developer | |
| See the resources | Viewer | |
| Connect, change the scope, turn off, test the connection | Administrator | |

## How it works

1. You enter a scope. Before it saves anything, Forgeline proves the login can read the scope.
2. The only commands it runs are `gcloud asset search-all-resources`, `gcloud auth list` and
   `gcloud config get-value project`.
3. The explorer groups what it finds by kind. Results are cached for 60 seconds; **Refresh** reads again.

## Connect

On the server, as the user Forgeline runs as:

1. Sign in: `gcloud auth login` (or `gcloud auth activate-service-account`).
2. Make sure that login has the **Cloud Asset Viewer** role (`roles/cloudasset.viewer`) on the scope you want, and
   permission to use the project's APIs.
3. Enable the Cloud Asset API: `gcloud services enable cloudasset.googleapis.com`.

Then in the console, enter the scope (a bare project id such as `my-project` works, or `projects/my-project`,
`folders/1234567890`, `organizations/1234567890`) and choose **Connect**. If the server's `gcloud` has a default
project, it is filled in for you; Forgeline reads only its name. If the check fails it says what to fix: no login, no
permission, the API not enabled, or an unknown scope.

Once connected it is listed under **Settings → Connections**, where **Test connection** runs the checks again at any
time. It only reads.

## What you see

An explorer of everything in the scope, grouped by kind: Compute Engine VMs, Cloud Run, GKE clusters, Cloud Storage
buckets, disks, Cloud SQL, BigQuery datasets, VPC networks, firewall rules, Pub/Sub topics, Secret Manager secrets
(names only) and **Vertex AI endpoints and models** under an AI group. A resource type Forgeline does not know yet is
listed under **Other resources** with its type, so nothing is hidden. At most 200 resources per kind are shown, and the
true count is reported.

![The Google Cloud explorer]({{ '/console/connect-gcp.png' | relative_url }})

## Security model

- **Read-only.** Only the three commands above run. Nothing in Google Cloud is created, changed or deleted.
- **Proven before saved.** A connection that cannot read the scope is refused.
- **Secrets by name only.** Secret Manager secrets are listed by name; their values are not read.

## Limits and verification

{: .warning }
**Verified:** the explorer, the connection and the test against a stand-in `gcloud` (`scripts/dev/fake-gcloud`),
including permission, login, missing-API and not-installed errors, and that only the three read commands ever run.
**Not verified:** a real Google account. The exact shape of the real `gcloud asset search-all-resources` output is
assumed from its documentation and handled defensively. **Not built yet:** the map view and Guard rules for Google
Cloud, assistant lookups, spend, changes (writes), and signing in from the console or with workload identity
federation.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| Connect says there is no login | `gcloud auth login` was not run as the user Forgeline runs as. |
| Connect says there is no permission | The login lacks **Cloud Asset Viewer** on the scope. |
| Connect says the API is not enabled | Run `gcloud services enable cloudasset.googleapis.com`. |
| Connect says the scope is unknown | Check the project id, or the `projects/`, `folders/` or `organizations/` form. |
| A kind shows fewer resources than exist | At most 200 per kind are shown; the count is the true one. |

## Related

- [Connect Azure](connect-azure.md)
- [Connect AWS](connect-aws.md)
- [People and roles](people-and-roles.md)
- [API: Google Cloud](../api/reference.md)
