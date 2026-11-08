# 032: Approval rules

Status: **Built (slice 1).** Date: 2026-11-08. Builds on notes 006 (cloud changes need a second administrator),
016 and 021 (AI proposals, per-account writes), 029/030 (the maintainer's enterprise focus: "approval rules
replace auto-apply as the way to tune governance") and 031.

## Question

Every change waits for one administrator who did not propose it. An enterprise wants more for what matters
("production needs two people", "only the platform leads approve the primary account", "deleting a stack needs
two") and no more friction for the rest. How do we let an administrator say that, without a second approval path?

## Decisions

| Decision | Choice |
| --- | --- |
| What a rule is | A name, what it matches (accounts, by id such as `primary`; kinds: `ai` for changes the assistant drafted, `cloud` for console cloud changes; optionally high-risk changes only), how many approvals it needs (1 or 2), and optionally which administrators may approve (usernames). An empty match means every change |
| No rules | Today's behaviour exactly: one approval from an administrator who did not propose it |
| Several rules | All matching rules apply and the strictest wins: the most approvals any of them asks, and an approver must be on every list that names approvers |
| What never changes | The approver is an administrator; the proposer can never approve; each approver approves the exact text (the hash); every check that runs at approval today (account still the same, services allowed, script rules, 24-hour expiry) still runs |
| Two approvals | The first approval is recorded with its name, time and note, and the change stays waiting ("1 of 2"). The same person cannot approve twice. The change runs only at the approval that completes the count. Any eligible administrator can still reject it. A proposal that expires keeps its approvals in the record and does not run |
| Rules read when | At each approval, so a rule tightened between two approvals applies to the second |
| High risk | AI proposals already carry a risk (`normal` or `high`); for console changes, deleting a stack is high risk |
| Not covered | Environment requests (their own self-service rules, note 017); the environment engine acting under such a rule; change windows; approvers by group (once groups come from single sign-on) |
| Who changes rules | Administrators. Every change is audited (`approval.rules`) with the rules before and after. Developers can read them, so they know who will approve |

## What was checked, and what was not

Checked by tests: no rules keeps one approval; a two-approval rule records the first approval and runs only on the
second; the same person cannot approve twice; the proposer cannot approve; a named-approver rule refuses others;
matching by account, kind and high risk; the strictest of several rules wins; console cloud changes follow the same
rules; rules are validated and only administrators change them. Each rule was removed in turn and a test failed.

Not checked: two real administrators approving one change in two browsers on the Pi (checked with two test
accounts in the test suite).
