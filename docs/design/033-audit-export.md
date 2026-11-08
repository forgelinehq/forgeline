# 033: Audit export and retention

Status: **Built (slice 1).** Date: 2026-11-08. Builds on note 007 (the audit log) and 029/030 (the maintainer's
enterprise focus). Follows 031 (single sign-on) and 032 (approval rules).

## Question

The audit log records who did what, including what was refused, and can only be read in the console. An auditor
wants a copy; a security team wants it in their SIEM; a company has a rule for how long such records are kept, and
sometimes a rule that they are not kept longer. How do we give them that without making the log editable?

## Decisions

| Decision | Choice |
| --- | --- |
| Export | `GET /v1/audit/export`, administrators only, as CSV (for a spreadsheet or an auditor) or JSON Lines (for a SIEM or a script), oldest first, with the same filters as the audit page plus a date range (`from`, `to`, both days included) |
| The export is recorded | An `audit.export` event with the count, format and range is written **before** the file is sent; if it cannot be written, nothing is sent. Nobody takes a copy unseen |
| Spreadsheet safety | The log holds text people typed. A CSV cell starting with `=`, `+`, `-`, `@`, tab or carriage return gets a leading `'`, so opening the file never runs a formula. JSON Lines keeps the text as it was |
| Size | At most 200,000 events per export; a larger match is refused with a message asking for a shorter range |
| Retention | Kept forever by default. An administrator can choose 30 to 3650 days. Older events are deleted once an hour (and at start). The minimum is enforced again when pruning, so a period written below 30 by hand still keeps 30 days; settings that cannot be read prune nothing |
| Pruning is recorded | Each prune writes `audit.pruned` with how many events and up to which day; each change of the period writes `audit.retention` with before and after. Shortening the period asks for confirmation in the console, suggesting a download first |
| Never | No API edits or deletes single events. Pruning removes only by age |
| Not yet | Forwarding each event as it happens (webhook or syslog to a SIEM); signing exports or chaining event hashes so tampering with the stored log is detectable; per-action retention (for example keeping sign-ins shorter) |

## What was checked, and what was not

Checked by tests: only administrators export or change retention; CSV is valid, quoted, oldest first and limited to
the range; formulas are defused; JSON Lines keeps the original text; filters and the inclusive `to`; every export,
prune and change is audited; nothing is pruned by default, below 30 days, or when the settings are unreadable;
validation of the period. Each of 13 rules was removed in turn and a test failed.

Not checked: exporting a very large log on the Pi (the export reads the whole log into memory; fine for hundreds of
thousands of events, not for tens of millions).
