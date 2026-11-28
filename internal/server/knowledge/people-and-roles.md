---
title: People and roles
parent: Guides
nav_order: 7
---

# People and roles

Every person who signs in to Forgeline has one of three roles: viewer, developer or administrator. The role decides what
the console and the API let them do. Administrators add people, change roles, reset passwords and disable accounts.

Use this page when you add the first people to a server, or when you need to know who may do what. To let people sign in
with a company login instead of a password, see [Single sign-on](single-sign-on.md).

## At a glance

| | |
| --- | --- |
| Where in the console | **Settings → People and roles** (the page is titled **People**); **Settings → API tokens** for your own tokens |
| Needs | Nothing beyond the server |
| Design | [Note 006](../design/006-console.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See runs, pipelines, infrastructure | Viewer | |
| Run, cancel and edit pipelines | Developer | |
| Delete a pipeline, agent, skill, team, workflow, schedule, eval suite or blueprint | Developer | Only what they created. An administrator deletes anything |
| Create and change agents, skills, teams, workflows, schedules and evals; publish agent versions | Developer | What a developer wrote runs with at most a developer's rights ([note 054](../design/054-developers-write-agents.md)); approvals administrators set up stay theirs |
| Write runbooks | Developer | A different administrator reviews them |
| Budgets, publishing gates, webhook secrets, MCP servers, agent registries and pushes | Administrator | |
| Propose cloud changes, ask the assistant | Developer | |
| List secret names | Developer | Names only, never values |
| Create or delete secrets, connect GitHub and AWS | Administrator | |
| Approve changes | Administrator | Not the person who proposed the change |
| Read the audit log | Administrator | |
| Add, change, disable or remove people | Administrator | The last administrator cannot be removed or demoted |
| Make a personal API token | Viewer | At most your own role; needs a browser session |
| Revoke a personal API token | Its owner, or an administrator | |
| List everyone's tokens | Administrator | Through the API only (`GET /v1/tokens?all=1`); the console shows your own |

The server token counts as an administrator named `token`.

## How it works

1. An administrator adds a person with a username, name, password and role.
2. The person signs in with the password. A role is checked on every request, in the console and in the API.
3. An administrator can later change the role, reset the password, disable the account or remove it.
4. Disabling an account, resetting its password, or removing it ends that person's sessions at once. Disabling or
   removing it also deletes the person's API tokens. Changing a role does not sign anyone out.

![People and their roles]({{ '/console/people-and-roles.png' | relative_url }})

## Add a person

1. Open **Settings → People and roles** and choose **Add person**.
2. Enter a username (letters, digits and `. _ - @`), a name, a temporary password of at least 12 characters, and a role.
3. Give the person the password. They can change it under their own account.

A username that exists is refused with `that username is taken`; a short password with
`password must be at least 12 characters`.

## Change what someone may do

1. Open **Settings → People and roles**.
2. Change the role in the person's row, or choose **Reset password**, **Disable** (**Enable** brings the account back) or
   the bin to remove them.

The server always keeps at least one active administrator. Demoting, disabling or removing the last one is refused with
`this is the last active administrator; make another one first`.

## Personal API tokens

Under **Settings → API tokens** anyone can make a token for AI tools that connect over MCP. A token has a name, a role
(at most your own, your own by default) and a life of 1 to 365 days (90 by default). The secret is shown once. Each
person can have at most 10 (`you already have 10 tokens; revoke one first`). You make a token from a browser session, not
with another token: the server token is refused with `sign in to the console as a person to create a token`, and a personal token
is refused with `sign in required`, because a personal token works only on `/mcp` and on no other API path.

## Security model

- Passwords use PBKDF2-SHA256 and are at least 12 characters, with login throttling per account and per address.
- Cookies are `HttpOnly` and `SameSite=Strict`. Requests that change something must send `X-Forgeline-Request: 1`.
- Secrets are encrypted with AES-256-GCM and cannot be read back; see [Secrets and memory](secrets-and-memory.md).
- Every change, refusal and failed login goes to the [audit log](audit-log.md).

See [design note 006](../design/006-console.md) for the rules and the known limits.

## Limits and verification

{: .warning }
**Run on the UAT instance on 2026-11-27 (commit `487f373`).** A viewer was added, signed in, refused the developer and administrator actions
in the table, given a personal token that worked on `/mcp` only, promoted (the session stayed), disabled (the session and the token stopped working at once),
re-enabled and deleted, with the server token as the administrator ([note](../testing/2026-11-27-uat-cloud-security.md)). **Not checked:** the console's People page itself (the test user is not an
administrator), the last-administrator refusal, login throttling, and the token limit of 10.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| Someone cannot see **Settings → People and roles** | The page is for administrators only. |
| You cannot demote or remove an account | It is the last active administrator. |
| Someone is signed out suddenly | An administrator disabled the account, reset the password or removed it, or the session reached its 12 hours. |
| Sign-in says `too many attempts; try again in ...` | Login throttling per account and per address. |
| Someone is refused when they approve a change | They proposed it themselves, or an [approval rule](approval-rules.md) names other approvers. |

## Related

- [Single sign-on](single-sign-on.md)
- [Approval rules](approval-rules.md)
- [Audit log and retention](audit-log.md)
- [Policy packs](policy-packs.md)
- [API: users and tokens](../api/reference.md)
