---
name: docs-review
description: Review and update the Forgeline guides after a change to the console or the API. Use after changing internal/server/ui, an API route or a setting, before opening a pull request, or when asked whether the documentation is up to date.
---

# Review the docs against the code

The guides in `docs/guides/` are one page per feature, all in the same layout (`docs/guides/runbooks.md` is the model).
Code changes make them wrong quietly. This skill finds which pages a change touched and brings them back in line.

## 1. Find what changed

```sh
git diff --stat origin/main...HEAD -- internal/server cmd schema docs/api
```

Map each changed file to a feature page. `internal/server/ui/pages/<name>.js` is the console page; `docs/guides/` has the
page named after the feature (look in `docs/guides/index.md`). A changed route, request field, role, limit, default or
error message is a change to the guide's **Who can do what**, **How it works**, **Limits** or **Troubleshooting**.
A new page or setting with no guide needs one.

## 2. Check each page against the code, not against the old text

For every claim the change could affect, read the code (the handler, the role check, the validation) and compare:
roles, limits, defaults, names of buttons and tabs (`internal/server/ui/lib/nav.js`, `settingsnav.js`), error messages.
Fix the page. Never write that something works unless a test or a run shows it; keep or add the page's `{: .warning }`
callout saying what was checked and what was not. Keep the layout: summary, At a glance, Who can do what, How it works,
steps, Security model, Limits and verification, Troubleshooting, Related.

## 3. Pictures

A picture is wrong when the page it shows has changed. Retake it from a running console (`scripts/dev/README.md` has the
stand-ins) and crop it to the part the text discusses:

```sh
python3 scripts/docs/crop.py <source.png> <feature-page-name.png> <y0> <y1>
```

At most two per page. Delete the full-window source afterwards: an unused picture fails the docs test.

## 4. API reference

`docs/api/reference.md` rows are checked against the routes by a test, but not the descriptions or fields. For each route
the change touched, read the handler and the request struct and correct the row.

## 5. Keep the copies in step and prove it

```sh
FORGELINE_UPDATE_KNOWLEDGE=1 go test ./internal/server -run TestKnowledgeMatchesTheDocs   # the assistant's copy
node --test site/scripts/sync.test.mjs site/scripts/docs.test.mjs                          # links, pictures, sync
cd site && npm ci && npm run build                                                         # the site builds
```

## 6. Report

Say which pages changed and why, which claims you checked in the code, and which you could not check. Do not say the
documentation is up to date; say what was compared.
