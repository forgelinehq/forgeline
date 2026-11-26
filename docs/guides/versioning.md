---
title: Versioning
parent: Guides
nav_order: 8
---

# Versioning

Forgeline versions three different things, and they move independently: the product, the HTTP API, and the skills,
agents and workflows you save.

Read this page to know what a version number promises, what a saved agent version pins, and how to roll back.

## At a glance

| | |
| --- | --- |
| Where | `forgeline version`; the Platform tab in the console; version history on each skill, agent and workflow |
| Needs | Nothing beyond the server |
| Design | [Note 019](../design/019-signed-packages.md) for signed packages |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See a skill's, agent's or workflow's versions | Developer | |
| Save a new version of a skill, agent or workflow | Administrator | An unchanged skill save is refused |
| Restore an old pipeline version | Developer | Makes it the newest |
| Roll back | Administrator | Saves a copy of the old version as the newest |
| Cut a product release | Maintainer | Tag on `main`; see *Cutting a release* |

## How it works

### The product

Releases follow [Semantic Versioning](https://semver.org/): `MAJOR.MINOR.PATCH`.

- **PATCH**: fixes, no behaviour change you would notice.
- **MINOR**: new features, new API routes, new optional fields. Existing pipelines and clients keep working.
- **MAJOR**: anything that breaks a pipeline file, the HTTP API or stored data.

Until 1.0.0, a MINOR release may include a breaking change, and the changelog says so under
**Changed**. `forgeline version` and the Platform tab in the console show the running version.
A build between tags shows `v1.2.3-4-gabc123`, meaning four commits after `v1.2.3`; `-dirty`
means it was built with uncommitted changes.

#### Cutting a release

1. Move the `Unreleased` section of `CHANGELOG.md` under a new heading with the version and date.
2. Merge that to `main`, then tag it: `git tag -a v0.1.0 -m "v0.1.0" && git push origin v0.1.0`.
3. The `release` workflow tests, builds binaries for Linux, macOS and Windows (amd64 and arm64),
   writes `SHA256SUMS`, and publishes a GitHub release with the changelog section as its notes.

### The HTTP API

Routes live under `/v1`. Within `/v1` we add routes and optional fields but do not remove or change
the meaning of existing ones. A breaking change would be `/v2` served alongside `/v1` for at least
one MINOR release. `docs/api/reference.md` lists every route and a test fails if one is missing.

### Skills, agents and workflows

Each skill, agent and workflow keeps every version it has ever had:

- A save creates the next version. Old versions are never edited.
- Each version has a fingerprint (a SHA-256 of its content), who saved it, when, and a note.
- An agent version **pins** the exact skill versions it uses, so the same agent version behaves the
  same way later even after a skill changes. The agent page shows when a newer skill version exists;
  saving a new agent version takes it.
- **Rollback** saves a copy of an old version as the newest one, so the history only grows.
- A workflow version pins the exact agent version of each step, so an agent change never alters a saved workflow until someone saves a new version of it.
- A run records which agent version and fingerprint answered, in the audit log and the reply.
- An agent version cannot gain a tool or a higher tier without a new saved version, which is audited.

Library packages use the same rules plus a pinned hash: an install is bound to the hash the administrator was
shown, installing only creates ordinary versioned agents and skills, and an update can never change what is already
installed (or raise an agent's tier) without someone choosing it. Signed packages are described in
[note 019](../design/019-signed-packages.md).

## Security model

- Old versions are never edited, so a saved version is what it was when it was saved. Each has a SHA-256 fingerprint,
  who saved it, when, and a note.
- An agent version pins exact skill versions, and a workflow version pins exact agent versions, so a change upstream
  never alters a saved version until someone saves a new one.
- An agent version cannot gain a tool or a higher tier without a new saved version, which is audited.
- A run records which agent version and fingerprint answered, in the audit log and the reply.
- A library package install is bound to the hash the administrator was shown, and an update can never change what is
  already installed without someone choosing it.

## Limits and verification

Until 1.0.0, a MINOR release may include a breaking change, and the changelog says so under **Changed**.
`docs/api/reference.md` lists every route, and a test fails if one is missing or has a different role.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| A version shows as `v1.2.3-4-gabc123` | The build is four commits after `v1.2.3`. |
| A version ends in `-dirty` | It was built with uncommitted changes. |
| An agent still uses an old skill | It pins the skill version. The agent page shows when a newer one exists; saving a new agent version takes it. |
| A save of a skill is refused | The content is unchanged. |

## Related

- [Write pipelines](pipelines.md)
- [Models and providers](models-and-providers.md)
- [People and roles](people-and-roles.md)
- [API reference](../api/reference.md)
