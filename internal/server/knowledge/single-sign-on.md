---
title: Single sign-on
parent: Guides
nav_order: 7
---

# Single sign-on

People can sign in with your company login instead of a Forgeline password: Google Workspace, Microsoft Entra ID,
Okta, Keycloak or any provider that speaks OpenID Connect. Forgeline can make an account at a person's first sign-in
and give it a role from the person's groups.

Use it when people already have a company login and you do not want a second password for them to manage.

## At a glance

| | |
| --- | --- |
| Where in the console | **Settings → Sign-in (SSO)** |
| Needs | An OpenID Connect provider where you can register a web application |
| Design | [Note 031](../design/031-single-sign-on.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| Read or change the settings, test the provider | Administrator | The client secret is never shown again |
| Sign in with the company login | Anyone the rules let in | |

## How it works

1. The sign-in page sends the browser to the provider, with PKCE, a single-use state and a nonce.
2. The provider sends the browser back to `/v1/auth/oidc/callback`. Forgeline checks the state, exchanges the code and
   verifies the ID token (signature, issuer, audience, expiry, nonce).
3. Forgeline applies your rules: whether new people may come in, which email domains, and which role their groups give.
   A person is matched by the provider's id for them. A first sign-in makes an account (audited as `user.create`); a
   later one updates the name, email and role.
4. It sets the session cookie and opens the console.

## Set it up

An administrator does this under **Settings → Sign-in (SSO)** in four steps.

1. **Register Forgeline with your provider.** First fill in **The address people reach Forgeline at** (`https`, or `http`
   on this machine only); the redirect address shown is built from it. Create a web application (an "OIDC client") at the
   provider with that redirect address (`https://<your Forgeline>/v1/auth/oidc/callback`), and copy its client ID and
   secret.
2. **Connect.** Enter the provider's issuer URL (for example `https://accounts.google.com`, or
   `https://login.microsoftonline.com/<tenant>/v2.0` for Entra ID) and press **Test**: it reads the provider's
   settings and says whether sign-in can work. Enter the client ID, the client secret (stored encrypted, never shown
   again; leave it empty later to keep it) and optionally the **Button text** shown as "Sign in with ...".
3. **Decide who may come in.** **Let new people in** makes an account at a person's first sign-in. **Roles from
   groups** maps your company groups to Forgeline roles (the highest match wins); **When no group matches** gives a
   default role or refuses them. **Allowed email domains** optionally narrows it to verified addresses in your
   domains.
4. **Turn it on.** Tick **Single sign-on is on** and choose **Save**. The sign-in page then shows **Sign in with …**
   above the password form. Turning it on is refused until the issuer, client ID, address and secret are filled in
   (`to turn single sign-on on, give the client secret`).

Worked example: a company with the group `forgeline-admins` maps it to Administrator, `forgeline-dev` to Developer, and
sets **When no group matches** to Viewer. A person in both groups gets Administrator. With **When no group matches** set
to "Nobody: refuse them", someone in neither group is refused.

![Single sign-on settings]({{ '/console/single-sign-on.png' | relative_url }})

## What to know

- Roles are applied at every sign-in: take someone out of a group and their role changes next time they sign in.
  The last administrator is never demoted this way (the audit log records `kept as admin`).
- Someone no group lets in (and with no default role) is refused at their next sign-in. A session they already have ends when it expires (12 hours) or when an
  administrator disables the account under [People and roles](people-and-roles.md), which ends it at once.
- Password sign-in keeps working for Forgeline accounts, so a broken provider never locks you out. Accounts made by
  single sign-on have no password and show an **SSO** badge under **People**.
- A disabled account stays refused: the audit log says `the account <name> is disabled`.
- To try it on a laptop without a real provider, run the stand-in in `scripts/dev/fakeoidc` (see its README).

## Security model

- **Matched by the provider's id.** Forgeline knows a person by the provider's own id for them, never by email alone,
  so changing an email at the provider cannot take over someone else's account.
- **Client secret sealed.** It is stored encrypted in the vault and never returned.
- **Https only.** The issuer and address must be `https`, or `http` on a loopback host.
- **Refusals audited.** A failed sign-in is recorded as `login.sso.failed` with the full reason.

## Limits and verification

{: .warning }
**Checked against a stand-in only.** The flow was checked in a browser against the stand-in provider in
`scripts/dev/fakeoidc`. It was **not** checked against a real provider (Google, Entra ID, Okta).

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| **Test** says `Could not read the provider at that address` | The issuer URL is wrong or the provider's settings cannot be read. |
| Save says `issuer: use an https address (http only for a provider on this machine)` | The issuer is `http` on a non-local host. |
| Sign-in is refused | The sign-in page shows one short message and the audit log has the full reason (`login.sso.failed`). The messages are `Single sign-on is not set up on this server.`, `The company login could not be reached, or it said no. Try again, or ask an administrator.`, `That sign-in expired or was started in another browser. Try again.`, `The company login sent back something Forgeline could not verify. Ask an administrator to check the single sign-on settings.` and `You signed in at your company, but this server does not let you in. Ask an administrator for access.` The last one usually means no matching group and no default role, a domain that is not allowed, or new people not being let in. |
| The button does not appear on the sign-in page | Single sign-on is not turned on, or its settings are incomplete (it needs the issuer, client ID, address and a stored secret). |
| Someone still has access after leaving the company | Their existing session lasts up to 12 hours; disable the account to end it now. |

## Related

- [People and roles](people-and-roles.md)
- [Audit log and retention](audit-log.md)
- [API: single sign-on](../api/reference.md)
