---
title: Connect GitHub
parent: Guides
nav_order: 3
---

# Connect GitHub

Pushes to chosen branches start runs of the `forgeline.yaml` at the repository root, and the result is posted back as a
commit status. Pull requests from forks never start runs.

Connect GitHub when you want Forgeline to build and test what you push, or when you want to use
[GitHub Actions](github-actions.md) from the console. There are two ways in: a GitHub App (recommended) and a token.

## At a glance

| | |
| --- | --- |
| Where in the console | **Settings → Connections → GitHub** (under **Add a connection** until it is connected) |
| Needs | A GitHub account or organization; a public **https** address for webhooks |
| Who | Administrator |
| Design | [Note 010](../design/010-github-app.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See the settings (no secrets) and recent deliveries | Developer | |
| Create the App, install it, change the rules, test, disconnect | Administrator | |
| Turn on **Secrets for pushes** for a repository | Administrator | Off by default |

## How it works

1. GitHub sends a webhook to `/webhooks/github` when someone pushes. Forgeline checks the signature with the webhook
   secret; a bad signature gets `401`.
2. If the repository and branch are on the allow list, Forgeline downloads that commit and runs the
   [pipeline](pipelines.md) in `forgeline.yaml`.
3. The result is posted back as a commit status.
4. The deliveries list shows every webhook as accepted, ignored or rejected, and why.

![The connected App, the allow list and the webhook instructions]({{ '/console/connect-github.png' | relative_url }})

## Connect with a GitHub App (recommended)

1. Click **Create the GitHub App**. Enter the owner (your account or an organization) and the address people reach this
   server at.
2. The console sends you to GitHub with the app's settings filled in. Approve once.
3. GitHub sends you back; Forgeline stores the app's key and webhook secret in its vault.
4. Click **Install**, pick the installation, then tick the repositories and branches that may start runs.

Forgeline mints short-lived tokens by itself, so there is no personal token to rotate.

{: .note }
GitHub can only deliver webhooks to a public **https** address. On a laptop the webhook is created switched off; put the
server behind TLS or a tunnel and switch it on.

## Connect with a token

Create a fine-grained token with *contents: read* and *commit statuses: write*, paste it with a webhook secret, add
`owner/repo:branch` rules, and use **Test connection**. The token is write-only.

## Security model

- **Allow list.** Only listed repositories and branches start runs. Pull requests from forks never do.
- **Trust.** A pull-request run gets no secrets. A push run gets secrets only where an administrator turned on
  **Secrets for pushes** for that repository. See [Write pipelines](pipelines.md).
- **Signed webhooks.** An unsigned or wrongly signed delivery is rejected.
- **No stored personal token** with the App: it uses short-lived installation tokens. Keys and secrets live in the
  vault, and a token is write-only.

## Limits and verification

{: .warning }
**Verified:** the whole flow against a stand-in for GitHub, including a signed webhook starting a run and statuses
posted with an installation token. The app was also created and installed on github.com, and the connection test read 19
repositories with an installation token. **Not yet:** a real webhook delivery or a commit status on a real repository.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| The webhook never arrives | GitHub can only deliver to a public **https** address. On a laptop the webhook is created switched off. |
| A delivery shows as rejected | The signature did not match the webhook secret. The deliveries list gives the reason. |
| A delivery shows as ignored | Most likely the repository or branch is not on the allow list. The deliveries list says why. |
| **Test connection** fails | Check the token's permissions (*contents: read* and *commit statuses: write*) and the webhook secret. |

## Related

- [GitHub Actions](github-actions.md)
- [Write pipelines](pipelines.md)
- [People and roles](people-and-roles.md)
- [API: GitHub](../api/reference.md)
