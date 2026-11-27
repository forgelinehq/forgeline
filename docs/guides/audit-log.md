---
title: Audit log
parent: Guides
nav_order: 19
---

# Audit log

The audit log records who did what in Forgeline, including everything that was refused and every failed sign-in. It can
be read and downloaded in the console and never edited. Administrators can filter it, download it as CSV or JSON Lines
for an auditor or a SIEM, and choose how long it is kept.

Use it to answer "who changed this and when", to review refusals, and to hand an auditor a copy.

## At a glance

| | |
| --- | --- |
| Where in the console | **Security → Audit log** |
| Needs | Nothing to set up. Events are written from the first sign-in |
| Design notes | [033 audit export and retention](../design/033-audit-export.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| Read and filter the log | Administrator | |
| Download CSV or JSON Lines | Administrator | Each download is itself recorded, before the file is sent |
| Choose how long it is kept | Administrator | 0 (forever) or 30 to 3650 days |
| Edit or delete one event | Nobody | There is no way to do it |

## How it works

1. Every change, refusal and failed login writes an event: when, who, what (an action name such as `run.start` or
   `pipeline.create`), what it was on, a detail and where it came from.
2. The page shows the newest 200 events. **Show older events** goes back as far as the log goes, with the filters
   applied.
3. Events older than the retention period are deleted once an hour. By default nothing is deleted.

![The audit log with filters, download buttons and the newest events]({{ '/console/audit-log.png' | relative_url }})

## Find events

1. Open **Security → Audit log**.
2. Filter by **Person**, **Action** (a prefix, for example `secret`), **From** and **to** dates, or tick **Only refused
   actions**.
3. Choose **Refresh** to read again.

Some useful actions to filter by:

| Action | Records |
| --- | --- |
| `run.secret.grant` | A step was given secrets: the run, the step, the secret names (never values), the network it has and how the run started |
| `run.secret.network` | A step got secrets and network access; filter by `run.secret` to find these at once |
| `run.secret.refused` | A step was refused a secret, with the reason |
| `audit.export` | A download of the log, with what was taken |
| `login.sso.failed` | A failed single sign-on, with the full reason |

## Download the log

1. Choose **Download CSV** (for a spreadsheet or an auditor) or **JSON Lines** (for a SIEM or a script).
2. The file has every event that matches your filters, oldest first, not only the 200 shown, up to 200,000 events. For a
   larger match, narrow the date range.

In the CSV, a cell that would start a spreadsheet formula gets a leading `'`, so opening the file never runs one. JSON
Lines keeps the text as it was.

## Choose how long it is kept

1. At the bottom of the page, find **How long to keep it**. The default is **Forever**.
2. Choose 90 days, 180 days, 1 year, 2 years or 7 years. The API takes any period from 30 to 3650 days; anything else is
   refused with `retention_days must be 0 (keep forever) or 30 to 3650`.
3. Confirm. Shortening the period asks first (**Delete older events**) and suggests downloading the log.

Older events are deleted once an hour and cannot be brought back. Each deletion is recorded as `audit.pruned` and each
change of the period as `audit.retention`.

## Security model

- **Append only.** No API edits or deletes a single event. Pruning removes only by age.
- **Seen exports.** An export writes `audit.export` before the file is sent; if that cannot be written, nothing is sent.
- **Refusals are events.** A refused action and a failed sign-in are recorded, with the reason.
- **Safe to open.** CSV cells that start `=`, `+`, `-`, `@`, tab or carriage return get a leading `'`.
- **Fail safe.** Nothing is pruned by default, below 30 days, or when the setting cannot be read.
- **Administrators only.** The log, the export and the retention setting need the administrator role.

## Limits and verification

{: .warning }
**Run on the UAT instance on 2026-11-27 (commit `487f373`):** the events of blueprint, cloud-change, user, secret, token and run work were all found by
filter, CSV and JSON Lines were downloaded, each download wrote `audit.export`, a developer was refused, and the page was not opened in a browser
(administrator only); retention was left alone ([note](../testing/2026-11-27-uat-cloud-security.md)).

{: .warning }
**Tested, but not at very large sizes.** Only administrators can export or change retention; CSV validity, ordering,
formula protection, filters and the audit of every export and prune are covered by tests, and each of 13 rules was removed
in turn to confirm a test fails ([note 033](../design/033-audit-export.md)). **Not checked:** exporting a very large log on
a small server. The export reads the whole log into memory, which is fine for hundreds of thousands of events, not for tens
of millions. Not built yet: forwarding events as they happen to a SIEM, and signing exports or chaining event hashes so
that tampering with the stored log can be detected.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| The Audit log tab is missing | It needs the administrator role. |
| An export is refused with `N events match; export at most 200000 at a time by choosing a shorter range` | More than 200,000 events match; narrow the dates. |
| Old events are gone | A retention period is set; events past it are deleted hourly. |
| A step's secret use is not found | Filter by action `run.secret`. |
| A CSV cell starts with `'` | That is the formula protection; the value is otherwise as logged. |

## Related

- [Home and approvals](home-and-approvals.md)
- [Secrets and memory](secrets-and-memory.md)
- [API reference](../api/reference.md)
