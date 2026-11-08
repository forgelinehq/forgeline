---
title: People and security
parent: Guides
nav_order: 7
---

# People and security

## Roles

| | viewer | developer | admin |
| --- | :-: | :-: | :-: |
| See runs, pipelines, infrastructure | ✓ | ✓ | ✓ |
| Run, cancel, edit pipelines | | ✓ | ✓ |
| Propose cloud changes, ask the assistant | | ✓ | ✓ |
| List secret names | | ✓ | ✓ |
| Create or delete secrets, connect GitHub and AWS, manage users, approve changes, read the audit log | | | ✓ |

The bearer token counts as an admin named `token`. Whoever proposes a cloud change cannot approve it.

![People]({{ '/console/11-people.png' | relative_url }})

## Single sign-on

People can sign in with your company login instead of a Forgeline password: Google Workspace, Microsoft Entra ID,
Okta, Keycloak or any provider that speaks OpenID Connect. An administrator sets it up under
**Settings → Sign-in (SSO)** in four steps:

1. **Register Forgeline with your provider.** Create a web application (an "OIDC client") there with the redirect
   address the page shows (`https://<your Forgeline>/v1/auth/oidc/callback`), and copy its client ID and secret.
2. **Connect.** Enter the provider's issuer URL (for example `https://accounts.google.com`, or
   `https://login.microsoftonline.com/<tenant>/v2.0` for Entra ID) and press **Test**: it reads the provider's
   settings and says whether sign-in can work. Enter the client ID and secret; the secret is stored encrypted and
   never shown again.
3. **Decide who may come in.** **Let new people in** makes an account at a person's first sign-in. **Roles from
   groups** maps your company groups to Forgeline roles (the highest match wins); **When no group matches** gives a
   default role or refuses them. **Allowed email domains** optionally narrows it to verified addresses in your
   domains.
4. **Turn it on.** The sign-in page then shows **Sign in with …** above the password form.

What to know:

- Forgeline knows a person by the provider's own id for them, never by email alone, so changing an email at the
  provider cannot take over someone else's account.
- Roles are applied at every sign-in: take someone out of a group and their role changes next time they sign in.
  The last administrator is never demoted this way.
- Someone who leaves cannot sign in again. A session they already have ends when it expires (12 hours) or when an
  administrator disables the account under **People**, which ends it at once.
- Password sign-in keeps working for Forgeline accounts, so a broken provider never locks you out. Accounts made by
  single sign-on have no password and show an **SSO** badge under People.
- If a sign-in fails, the sign-in page says why in plain words, and the audit log has the details
  (`login.sso.failed`).
- To try it on a laptop without a real provider, run the stand-in in `scripts/dev/fakeoidc` (see its README).

![Single sign-on settings]({{ '/console/84-sso-settings.png' | relative_url }})
![The sign-in page with single sign-on]({{ '/console/85-sso-sign-in.png' | relative_url }})

## Approval rules

Every change to your cloud already needs an administrator who did not propose it. Under
**Settings → Approval rules** you can ask for more where it matters:

- **Needs two approvals**: the first approval is recorded and the change keeps waiting ("Approve (1 of 2)"); it
  runs only when a second, different administrator approves the same exact change.
- **Who may approve**: tick the administrators who may approve; everyone else's approval is refused.
- A rule matches by **account** (for example Primary), by **who made the change** (the assistant, or people in the
  console) and optionally **only high-risk changes** (such as deleting a stack). Nothing ticked means every change.

When several rules match, the strictest wins: the most approvals any rule asks for, and only people allowed by every
rule that names approvers. With no rules, nothing changes. The Approvals page shows on each waiting change how many
approvals it needs, who already approved, and who may approve next. Developers can read the rules, so they know who
will review their changes; only administrators change them, and every change is in the audit log.

![Approval rules]({{ '/console/86-approval-rules.png' | relative_url }})

## What protects you

- Passwords use PBKDF2-SHA256, at least 12 characters, with login throttling per account and per address.
- Cookies are `HttpOnly` and `SameSite=Strict`. Requests that change something must send `X-Forgeline-Request: 1`.
- Secrets are encrypted with AES-256-GCM, cannot be read back, and are masked in logs.
- Steps get only the secrets and network they ask for. Runs started by GitHub get no secrets.
- Every change, refusal and failed login goes to the audit log.

See [design note 006](https://github.com/forgelinehq/forgeline/blob/main/docs/design/006-console.md) for the rules and the known limits.
