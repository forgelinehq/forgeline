---
title: Connect GitHub
parent: Guides
nav_order: 3
---

# Connect GitHub

Pushes to chosen branches start runs of the pipeline file at the repository root (`forgeline.yaml` unless you name another),
and the result is posted back as a commit status. Pull requests from the same repository start untrusted runs; pull
requests from forks never start runs.

Connect GitHub when you want Forgeline to build and test what you push, or when you want to use
[GitHub Actions](github-actions.md) from the console. There are two ways in: a GitHub App (recommended) and a token.

## At a glance

| | |
| --- | --- |
| Where in the console | **Settings → Connections → GitHub** (under **Add a connection** until it is connected) |
| Needs | A GitHub account or organization; a public **https** address for webhooks |
| Design | [Note 010](../design/010-github-app.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See the settings (no secrets) and recent deliveries | Developer | The form is read-only; secrets are never shown |
| Create the App, install it, change the rules, test, disconnect | Administrator | |
| Turn on **Secrets for pushes** for a repository | Administrator | Off by default |

## How it works

1. GitHub sends a webhook to `/webhooks/github` when someone pushes or opens a pull request. Forgeline checks the
   signature with the webhook secret; a missing or bad signature gets `401 missing or invalid signature`.
2. If the repository and branch are on the allow list, Forgeline downloads that commit and runs the
   [pipeline](pipelines.md) in the repository's pipeline file. A second delivery of the same event is ignored as a
   "duplicate delivery".
3. The result is posted back as a commit status.
4. **Recent deliveries** (the last 12) shows each webhook with its outcome (for example accepted, ignored, rejected or
   failed) and why.

![The connected App, the allow list and the webhook instructions]({{ '/console/connect-github.png' | relative_url }})

## Connect with a GitHub App (recommended)

1. Open **Settings → Connections → GitHub**. Under **Connect with a GitHub App** fill in **This server's public address**,
   optionally an **Organisation** (empty means your own account) and an **App name**, then choose **Create the GitHub App**.
2. The console sends you to GitHub with the app's settings filled in. Approve once. The App asks for read access to
   contents, pull requests and metadata, and write access to commit statuses, and subscribes to push and pull request
   events.
3. GitHub sends you back (**Step 2 of 2: install**); Forgeline stores the app's key and webhook secret in its vault.
4. Choose **Open GitHub to install**, install it on the account or organization, return and choose **I installed it:
   check again**. Pick the installation, tick the repositories that may start runs (each gets its default branch), and
   choose **Save and turn on**. Edit the branches in the rules below afterwards.

Forgeline mints short-lived tokens by itself, so there is no personal token to rotate. The App does not ask for the
**Actions** permission: to use [GitHub Actions](github-actions.md) you approve that on the App's page on GitHub.

{: .note }
GitHub can only deliver webhooks to a public **https** address. On a laptop the webhook is created switched off; put the
server behind TLS or a tunnel and switch it on.

## Connect with a token

Create a fine-grained token with *Contents: read* and *Commit statuses: write* on the repositories. In the form, paste it
under **Access token**, choose **Generate a new one (shown once)** or type your own webhook secret (at least 16
characters), and list each repository as `owner/repository` with its branches (`main, release/*`; `*` does not cross a
`/`). Choose **Save**, then **Test connection**, which checks each repository. The token is write-only. Add the webhook
in GitHub yourself: repository → Settings → Webhooks → Add webhook, with the payload URL the page shows, content type
`application/json`, the secret, and the events **Pushes** and **Pull requests**.

Other fields: **Enabled**, **Pipeline file in each repository**, and **GitHub API address** (change only for GitHub
Enterprise Server). **Disconnect** (a red button) removes a connection made in the console.

## Security model

- **Allow list.** Only listed repositories and branches start runs. Pull requests from forks are ignored ("pull request
  from a fork; its code is untrusted").
- **Trust.** A pull-request run gets no secrets. A push run gets secrets only where an administrator turned on
  **Secrets for pushes** (a checkbox on the repository's row, with the branches). See [Write pipelines](pipelines.md#secrets).
- **Signed webhooks.** An unsigned or wrongly signed delivery is rejected.
- **No stored personal token** with the App: it uses short-lived installation tokens. Keys and secrets live in the
  vault, and a token is write-only.

## Limits and verification

{: .warning }
**Checked against the code for this review:** the console labels, roles, webhook responses and App permissions. Earlier
checks, as recorded: the whole flow against a stand-in for GitHub, including a signed webhook starting a run and statuses
posted with an installation token. The app was also created and installed on github.com, and the connection test read 19
repositories with an installation token. **Not yet:** a real webhook delivery or a commit status on a real repository.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| The webhook never arrives | GitHub can only deliver to a public **https** address. On a laptop the webhook is created switched off. |
| A delivery shows as rejected | The signature did not match the webhook secret. The deliveries list gives the reason. |
| A delivery shows as ignored | Most likely the repository or branch is not on the allow list. The deliveries list says why. |
| **Test connection** fails | Check the token's permissions (*Contents: read* and *Commit statuses: write*). Each repository is listed with its result. |
| `404 the GitHub integration is not configured` from the webhook | GitHub is not enabled; save the settings first. |
| `503 too many commits are being downloaded; redeliver in a moment` | Many deliveries arrived at once. Redeliver from GitHub. |
| "Needs attention" shows on the GitHub card | The page shows the reason in a red box above the form. |

## Related

- [GitHub Actions](github-actions.md)
- [Write pipelines](pipelines.md)
- [People and roles](people-and-roles.md)
- [API: GitHub](../api/reference.md)
