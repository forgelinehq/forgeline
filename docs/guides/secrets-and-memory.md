---
title: Secrets and memory
parent: Guides
nav_order: 17
---

# Secrets and memory

Two Settings pages hold things Forgeline keeps for you. **Secrets** stores keys and tokens that pipeline steps may use:
encrypted, never shown again, and masked in output. **Memory** shows what the assistant remembers: notes about you, notes
about this system, and your past conversations.

Use Secrets when a step needs a token or password. Use Memory to see, correct or delete what the assistant has kept.

## At a glance

| | |
| --- | --- |
| Where in the console | **Settings → Secrets**, **Settings → Memory** |
| Needs | Nothing to connect. The assistant needs a model for Memory to be useful |
| Design notes | [006 console](../design/006-console.md) (secrets), [027 assistant memory and history](../design/027-assistant-memory-and-history.md), [052 memory files](../design/052-assistant-that-works-like-a-colleague.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| List secret names, descriptions, who changed them and when | Developer | Values are never returned |
| Create, replace or delete a secret | Administrator | The value must be at least 8 characters |
| Read and add notes about yourself, your conversations | Developer | Notes about you and your conversations are private to you |
| Suggest a note about the system | Developer | It waits for an administrator to keep it |
| Keep, edit or delete a system note; write the organisation's memory files | Administrator | |
| Turn memory on or off | Administrator | |

## Secrets

### How it works

1. An administrator adds a secret: a name (letters, digits and underscores, not starting with a digit), a value and an
   optional description. The value is encrypted at rest.
2. A step lists the secret's name under `permissions.secrets`. Only then does it receive the value.
3. Nobody can read a value back. If a step prints one, its output shows `***` in its place.
4. Each grant is in the [audit log](audit-log.md): `run.secret.grant` names the run, the step and the secret names (never
   values).

### Add a secret

1. Open **Settings → Secrets** and choose **Add secret**.
2. Enter the name, the value (at least 8 characters, so it can be masked in logs) and what it is for.
3. Save. To change it later, add it again with the same name; to remove it, delete it.

To use it, list the name in a step's `permissions.secrets` (see [Write pipelines](pipelines.md#secrets)).

## Memory

### How it works

1. The assistant keeps short notes: about you (private) and about this system (shared). You can also add notes yourself.
2. Notes go into the assistant's prompt as background it was told came from people. It never treats a note as an order,
   and no note can change a role, a policy or a limit.
3. A note the assistant took from logs or cloud data, or after reading untrusted text, waits under **Keep this note?**
   for you to press **Keep**.
4. Your conversations are saved so you can open them again from the assistant's history.

| Part | Who sees it | Kept |
| --- | --- | --- |
| **About you** | Only you | Until you delete it |
| **About this system** | Everyone who uses the assistant; an administrator keeps or removes each one | Until removed |
| **Your conversations** | Only you, including from administrators | Up to 90 days, 100 at most |

### Add or remove a note

1. Open **Settings → Memory**.
2. Type a note of up to 300 characters, choose **About me** or about the system, and choose **Remember**. A developer's
   note about the system waits for an administrator.
3. Delete any note of your own at any time. An administrator can delete system notes. You can also forget every note
   about you in one step.

Passwords and keys are refused, as is markup. A note can carry a last day on which it is true (`valid_until`); after that
day it is kept as history but the assistant no longer uses it. For the memory files the assistant writes itself, see
[Assistant memory](assistant-memory.md).

![The Memory page with its three parts]({{ '/console/secrets-and-memory-memory.png' | relative_url }})

## Security model

- **Write-only secrets.** The API returns names and metadata, never values. Secrets are encrypted with AES-256-GCM.
- **Only what a step asks for.** A step receives only the secrets it lists. Pull-request runs from GitHub are untrusted
  and receive none; a push run receives them only where an administrator allowed it for that repository.
- **Masked.** Output that contains a secret is masked as `***`.
- **Notes are data.** Memory is never instruction, and cannot grant anything.
- **Untrusted sources wait.** A note learned from tool output is held until a person keeps it.
- **Audited.** Secret grants, refusals and changes are in the audit log; forgetting every note about you is audited as
  `memory.me.forget_all`, and turning memory on or off as `memory.settings`.

## Limits and verification

{: .warning }
**Memory was checked with a scripted model.** The rules were tested through the real server, with 30 rules removed in turn
to confirm a test fails, and in a browser. **Not checked:** a real model deciding when to save a note or how it uses
notes, an installation with very many conversations (they are found by listing and filtering), and phones
([note 027](../design/027-assistant-memory-and-history.md)).

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| A step does not get a secret | Its name is not in the step's `permissions.secrets`, or the run came from a GitHub pull request. |
| Adding a secret is refused | The value is under 8 characters, or the name has characters other than letters, digits and underscores. |
| A secret's value appears as `***` in output | That is the masking working. |
| The assistant did not save a note | Memory is off, the text looks like a password, key or markup, or it waits under **Keep this note?**. |
| A note you suggested about the system is not used | An administrator has not kept it yet. |
| A conversation is gone | Conversations are kept up to 90 days, 100 at most. |

## Related

- [Write pipelines](pipelines.md)
- [Assistant memory](assistant-memory.md)
- [Assistant](assistant.md)
- [People and roles](people-and-roles.md)
- [Audit log](audit-log.md)
