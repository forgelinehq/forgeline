# Contributing

Forgeline is pre-alpha and maintained by one person. Issues, design feedback and pull requests are welcome.

## Where to start

- Read the [README](README.md) for the idea, [`docs/ROADMAP.md`](docs/ROADMAP.md) for the milestones and
  [`docs/PLAN.md`](docs/PLAN.md) for what is next. [`docs/status.md`](docs/status.md) lists what has been checked on real
  services and what has not.
- Small fixes, tests and documentation improvements make good first pull requests.
- Changes to the pipeline spec, the trust model or the extension interfaces start as a numbered design note in
  [`docs/design/`](docs/design/) so they can be discussed before code is written.

## Build and test

- `make ci` runs vet, tests with the race detector, and a build. Run it before opening a pull request; CI runs the same
  thing plus `make css-check`.
- `make run-local` builds and starts the server and the console on 127.0.0.1:8080. Steps need Docker to run.
- The console stylesheet is generated. After changing a class name in `internal/server/ui`, run `make css` and commit
  `internal/server/ui/app.css`.
- Postgres tests run only when `FORGELINE_TEST_DATABASE_URL` is set. Run them when you change `internal/store` or
  `internal/docstore`.
- The documentation site is in `site/`. Write pages in `docs/`. After editing `docs/guides/`, `docs/get-started.md` or
  `docs/status.md`, run
  `FORGELINE_UPDATE_KNOWLEDGE=1 go test ./internal/server -run TestKnowledgeMatchesTheDocs` so the assistant's built-in
  guides match.
- After changing types in `internal/pipeline`, regenerate the JSON Schema with
  `go test ./internal/pipeline -run TestSchemaMatchesCommittedFile -update`. Never edit `schema/` by hand.

## Conventions

- Go 1.27. Run `gofmt` and `go vet`. Write table-driven tests. Error messages say what is wrong and where, for example
  `steps[2].needs[0]: unknown step "x"`.
- Keep commits small, with a message that explains why.
- Keep dependencies few. A new one needs an Apache-2.0-compatible license (MIT, BSD, Apache-2.0 or MPL-2.0; no GPL or
  AGPL) and a one-line reason in the commit message.
- Pin every third-party GitHub Action to a full commit SHA and keep `permissions: contents: read` by default.
- Never commit secrets, tokens or personal data. Leave cloud account numbers, hostnames, usernames and local paths out of
  the repository and out of screenshots.
- The console builds markup from text nodes. Do not use `innerHTML` or inline styles; a test enforces it.
- Security rules are tested by breaking them: after adding a check, remove it and confirm a test fails.
- Cloud changes happen only through proposal and approval. Do not add a path that changes the cloud directly.
- Do not claim a feature works until a test or a run shows it, and say what is unverified.

## Pull requests

- Work on a branch and open a pull request. Explain the reason for the change in the description.
- `make ci` must pass.
- When a change moves a milestone or what is verified, update `docs/ROADMAP.md` and `docs/status.md` in the same pull
  request.

## Security

Report vulnerabilities as described in [`SECURITY.md`](SECURITY.md), not in a public issue.

By contributing, you agree that your contribution is licensed under the Apache License 2.0.
