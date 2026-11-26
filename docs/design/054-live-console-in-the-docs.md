# 054: The live console in the documentation

Status: slice 1 built (shell and fixture replay); recorder, drift checks and guide migration follow.

## Problem

The guides show the console as 157 PNG files (21 MB) taken by hand. The console changes weekly, so the pictures are
old, and nothing notices. A fix that needs a person to retake pictures does not last for a one-person project.

## Decision

The documentation site serves the real console, built from `internal/server/ui` at deploy time, at `/demo/`. A guide
shows a page of it with a comment line, and the site turns that into a frame:

    <!-- console /pipelines "The pipelines page" -->

The page then cannot be older than the build. Dark mode, a phone width and the real fonts come for free.

## How it works

- `site/scripts/demo.mjs` copies the console into `site/public/demo/` (generated, not committed) and writes an
  `index.html` that loads `shim.js` first.
- `shim.js` replaces `fetch`. A `GET` is answered from `site/demo/fixtures.json`; anything that changes state gets a
  403 that says it is a demo. Timestamps in the answers move forward by the age of the recording, so "3 minutes ago"
  stays believable. A `GET` with no fixture is a 404 and is listed in `window.__demoMisses`, so a test can fail on it.
- `site/demo/record.mjs` (next slice) starts a real server with the stand-ins in `scripts/dev`, seeds it, walks the
  pages in a browser and writes the fixtures. A check re-records and fails if the committed file differs, and another
  fails if a page of the console has no frame in the guides and no exemption.

## Safety

- The demo is part of the site, never of the server binary: the shim and fixtures are not under `internal/server/ui`,
  so the console cannot be started in demo mode on a real server.
- Fixtures hold made-up data from a throwaway server: no real accounts, hostnames, usernames or paths (CLAUDE.md).
  The recorder refuses to write a fixture containing the maintainer's home directory or an AWS account number that is
  not on an allow list of the stand-ins' ids.
- The frame is on the Pages origin; the server's `frame-ancestors 'none'` still applies to the real console.

## Not done here

Streaming pages (run logs, the assistant) answer from one canned frame; they are shown as recorded, not live.
Theme sync between the site's switch and the frame is a later nicety.
