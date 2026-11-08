# 031: Single sign-on with OpenID Connect

Status: **Built (slice 1).** Date: 2026-11-08. Builds on notes 006 (accounts and roles), 012 (enterprise gaps:
"SSO blocks most enterprise evaluations") and 029/030 (the maintainer's enterprise focus). Facts about the code
are from `origin/main` at `b1954fb`.

## Question

Every person needs a Forgeline password today. A company wants people to sign in with the login they already
have (Google Workspace, Microsoft Entra ID, Okta, Keycloak…), to give roles from company groups, and to stop
access when someone leaves. How do we add that without weakening sign-in, and without locking anyone out?

## Decisions

| Decision | Choice |
| --- | --- |
| Protocol | OpenID Connect, authorization code flow with PKCE (S256), a random `state` (single use, 10 minutes) and a `nonce` checked in the ID token. Any provider with a discovery document works; nothing is provider-specific |
| Library | `github.com/coreos/go-oidc/v3` (Apache-2.0) to discover the provider and verify ID tokens against its published keys, and `golang.org/x/oauth2` (BSD-3-Clause) for the code exchange. Hand-written token checks are where sign-in bugs live |
| Who a person is | The pair (issuer, `sub`). That is stable at every provider. **Never the email alone**: some providers let a person choose their email, so matching on it would let them take over an existing account |
| First sign-in | A new account is created only when an administrator turned on "Let new people in"; its role comes from the group mapping, else the default role, else sign-in is refused with "ask an administrator". An optional list of allowed email domains narrows who may come in, and needs `email_verified` |
| Roles from groups | A claim (default `groups`) and a mapping from group name to role. The highest matching role wins and is applied at every sign-in, so removing someone from a group takes effect next time they sign in. A sign-in never demotes the last active administrator |
| Leaving the company | The provider refuses them, so they cannot start a new session. Existing sessions end when they expire or when an administrator disables the account (which already ends its sessions) |
| Passwords | Kept for local accounts, so a broken provider never locks an administrator out. An SSO account has no password; password sign-in for it fails like a wrong password |
| The client secret | Stored sealed in the vault (`sso.client_secret`), write-only like every secret; never shown or logged |
| The callback | `<address>/v1/auth/oidc/callback`, where the address is set with the provider (the page shows the exact URL to register). It must be `https`, except on a loopback address for trying it on a laptop. The issuer must be `https` too, with the same exception |
| Cookies | The session cookie stays `SameSite=Strict`. The short-lived cookie that ties the browser to the `state` is `SameSite=Lax`, because the provider sends the person back with a cross-site redirect |
| Audit | `login.sso` on success, `login.sso.failed` with the reason (state, token, refused by the rules), `sso.update` for settings, `user.create` for an account made at first sign-in, `user.role` when groups change a role |
| Who configures it | Administrators, under Settings › Sign-in (SSO). A Test button fetches the discovery document and says what it found |

## Not in this slice

SAML; SCIM (provisioning and removal pushed by the provider); "require SSO for everyone except a break-glass
account"; signing out at the provider; several providers at once; linking an existing password account to SSO
(an administrator can disable the old one).

## What was checked, and what was not

Checked by tests against a stand-in provider in the test (its own keys, discovery and token endpoint): a good
sign-in creates an account with the mapped role; a second sign-in is the same account and follows group changes;
wrong or reused `state`, a missing state cookie, a wrong `nonce`, a token signed by another key, a wrong
audience and an expired token are refused; sign-in is refused when new people are not let in, when the email
domain is not allowed or not verified, and when no role applies; the last administrator is not demoted; the
secret is never returned; only administrators change settings; plain-`http` issuers and callbacks are refused
unless loopback. Each rule was removed in turn and a test failed.

Checked in a browser against `scripts/dev/fakeoidc` (a stand-in provider for laptops). **Not checked against a
real provider** (Google, Entra, Okta): that needs an account registered with one.
