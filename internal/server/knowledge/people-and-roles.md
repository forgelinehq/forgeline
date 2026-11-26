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
| Where in the console | **Settings → People and roles**; **Settings → API tokens** for your own tokens |
| Needs | Nothing beyond the server |
| Design | [Note 006](../design/006-console.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See runs, pipelines, infrastructure | Viewer | |
| Run, cancel, edit pipelines | Developer | |
| Propose cloud changes, ask the assistant | Developer | |
| List secret names | Developer | Names only, never values |
| Create or delete secrets, connect GitHub and AWS | Administrator | |
| Approve changes | Administrator | Not the person who proposed the change |
| Read the audit log | Administrator | |
| Add, change, disable or remove people | Administrator | The last administrator cannot be removed or demoted |
| Make a personal API token | Viewer | At most your own role; needs a browser session |

The server token counts as an administrator named `token`.

## How it works

1. An administrator adds a person with a username, name, password and role.
2. The person signs in with the password. A role is checked on every request, in the console and in the API.
3. An administrator can later change the role, reset the password, disable the account or remove it.
4. Disabling an account, or resetting its password, ends that person's sessions at once.

![People and their roles]({{ '/console/people-and-roles.png' | relative_url }})

## Add a person

1. Open **Settings → People and roles** and choose **Add person**.
2. Enter a username, a name, a password of at least 12 characters, and a role.
3. Give the person the password. They can change it under their own account.

## Change what someone may do

1. Open **Settings → People and roles**.
2. Change the role in the person's row, or choose **Reset password**, **Disable** or the bin to remove them.

The server always keeps at least one active administrator.

## Personal API tokens

Under **Settings → API tokens** anyone can make a token for AI tools that connect over MCP. A token has a name, a role
(at most your own, your own by default) and a life of 1 to 365 days (90 by default). The secret is shown once. Each
person can have at most 10. You make a token from a browser session, not with another token. An administrator can see
and revoke everyone's tokens.

## Security model

- Passwords use PBKDF2-SHA256 and are at least 12 characters, with login throttling per account and per address.
- Cookies are `HttpOnly` and `SameSite=Strict`. Requests that change something must send `X-Forgeline-Request: 1`.
- Secrets are encrypted with AES-256-GCM, cannot be read back, and are masked in logs.
- Steps get only the secrets and network they ask for. Pull-request runs are untrusted and get no secrets; a push run
  gets them only where an administrator allowed it for that repository.
- Every change, refusal and failed login goes to the [audit log](audit-log.md).

See [design note 006](../design/006-console.md) for the rules and the known limits.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| Someone cannot see **Settings → People and roles** | The page is for administrators only. |
| You cannot demote or remove an account | It is the last active administrator. |
| Someone is signed out suddenly | An administrator disabled the account or reset the password. |
| Someone is refused when they approve a change | They proposed it themselves, or an [approval rule](approval-rules.md) names other approvers. |

## Related

- [Single sign-on](single-sign-on.md)
- [Approval rules](approval-rules.md)
- [Audit log and retention](audit-log.md)
- [Policy packs](policy-packs.md)
- [API: users and tokens](../api/reference.md)
