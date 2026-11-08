# Development stand-ins

Throwaway helpers for trying the console without touching real accounts. The Go tests do not use
them (they have their own fakes), except `fakeoidc`, which shares `internal/oidcfake` with the tests. Never expose them to a network.

## `fake-aws`: a stateful stand-in for the AWS CLI

Implements only the calls the console makes (`sts`, `cloudformation`, `ec2`, `ce`); stacks take a
few polls to finish. Put it first on `PATH` under the name `aws` and run a server:

```sh
mkdir -p /tmp/fake-aws/bin && ln -sf "$PWD/scripts/dev/fake-aws" /tmp/fake-aws/bin/aws
PATH=/tmp/fake-aws/bin:$PATH FAKE_AWS_STATE=/tmp/fake-aws/state.json \
  FORGELINE_TOKEN=a-long-dev-token-123 ./bin/forgeline server --listen 127.0.0.1:8081 --data-dir /tmp/fl-sim
```

To try several accounts, set `FAKE_AWS_ACCOUNTS="staging=444455556666"`: that profile is then a separate account with
its own state file. In the console create the administrator, a second administrator and a developer, connect AWS
(leave the profile empty, any region), and walk a proposal through approval. Delete the state
file to start over.

## `fake-tofu`: a stand-in for OpenTofu

For trying blueprints (note 035) with `fake-aws`: Preview changes, Send for approval and the apply all work, and nothing
is built anywhere. Its "state" is the list of resource addresses, so a second preview after an apply changes nothing.
Start the server with `FORGELINE_TOFU="$PWD/scripts/dev/fake-tofu"`; without it Forgeline downloads the real OpenTofu.

## `fakeoidc`: a stand-in single sign-on provider

```sh
go run ./scripts/dev/fakeoidc -listen 127.0.0.1:9111 -client forgeline -secret dev-secret
```

In the console, **Settings → Sign-in (SSO)**: issuer `http://127.0.0.1:9111`, client ID `forgeline`, client secret
`dev-secret`, Forgeline's address as you open it (for example `http://127.0.0.1:8081`), turn on **Let new people in**,
and map a group such as `platform-admins` to a role. Sign out and press **Sign in with …**: the stand-in shows a form
where you type who signs in (subject, email, groups). It signs in whoever you type, so keep it on loopback.

## `fake-github.py`: a stand-in for the GitHub API

```sh
python3 scripts/dev/fake-github.py 9911 /tmp/statuses.log
```

It also plays GitHub's side of **Connect with a GitHub App**: it serves the approval page, hands back
an app id, a webhook secret (`demo-webhook-secret-from-fake-github-0123`) and a private key, lists one
installation (`acme`, repository `acme/shop`) and issues installation tokens; each status it logs
says whether it was posted with `installation-token` or `personal-token`. In the console set the
GitHub API address in the *Connect with a GitHub App* card to `http://127.0.0.1:9911`, click
*Create the GitHub App*, then *Continue to GitHub* and *Create GitHub App*. It checks the app token's
shape and expiry but not its signature (the Go tests do).

It also serves the **GitHub Actions** calls for `acme/shop`: two workflows (one disabled), three seeded runs (one failed, one from a fork), `workflow_dispatch` (only on `main` or `release/1`; a started run goes queued, running, succeeded over about 15 seconds), cancel, re-run, jobs, and a log that is reached through a redirect to a second address. It prints `LEAK` to its stderr if the console ever sends its token to that second address.

It accepts the token `ghp_demo_token_value_1234`, knows one repository (`acme/shop`) whose commit
holds a small `forgeline.yaml`, and appends every commit status it receives to the log. In the
console set the API address to `http://127.0.0.1:9911` (plain http is allowed only for loopback),
the token to that value and the repository to `acme/shop`; saving shows a webhook secret once. Then
send a signed `push` event: the header `X-Hub-Signature-256: sha256=<HMAC-SHA256 of the body with
the secret>`, `X-GitHub-Event: push`, and a body like
`{"ref":"refs/heads/main","after":"<40 hex>","repository":{"full_name":"acme/shop"}}`.

## `fake-sso.py`: a stand-in for AWS IAM Identity Center sign-in

```sh
python3 scripts/dev/fake-sso.py 9922
FORGELINE_DEV_SSO_BASE=http://127.0.0.1:9922 PATH=/tmp/fake-aws/bin:$PATH \
  ./bin/forgeline server --listen 127.0.0.1:8081 --data-dir /tmp/fl-sim
```

In the console open Infrastructure → AWS account → Connect, enter any `https://d-….awsapps.com/start`
address, and open the link the page shows (it is served by this script) to approve. Then pick one
of the two demo accounts. The server refuses `FORGELINE_DEV_SSO_BASE` unless it is a loopback
address. Role credentials are fake, so use it together with `fake-aws`.

## `fake-gcloud`: a stand-in for the gcloud command

Answers the three read commands the console runs for Google Cloud (`asset search-all-resources`, `auth list`,
`config get-value project`) with made-up resources, including Vertex AI and a kind the console does not know.
Put it first on `PATH` as `gcloud`:

```sh
mkdir -p /tmp/fake-gcloud/bin && ln -sf $PWD/scripts/dev/fake-gcloud /tmp/fake-gcloud/bin/gcloud
PATH=/tmp/fake-gcloud/bin:$PATH FORGELINE_TOKEN=a-long-dev-token-123 ./bin/forgeline server --listen 127.0.0.1:8081 --data-dir /tmp/fl-sim
```

`FAKE_GCLOUD_DENY=1` makes the search fail with a permission error; `FAKE_GCLOUD_NOAUTH=1` makes `auth list` print nothing.

## `fake-az`: a stand-in for the az command

Answers the read commands the console runs for Azure (`account show`, `account list`, `graph query` with paging and
the `--subscriptions` filter) with made-up resources, including Azure OpenAI, an AI Foundry workspace and a kind the
console does not know. Put it first on `PATH` as `az`:

```sh
mkdir -p /tmp/fake-az/bin && ln -sf $PWD/scripts/dev/fake-az /tmp/fake-az/bin/az
PATH=/tmp/fake-az/bin:$PATH FORGELINE_TOKEN=a-long-dev-token-123 ./bin/forgeline server --listen 127.0.0.1:8081 --data-dir /tmp/fl-sim
```

`FAKE_AZ_DENY=1` makes the query fail with an authorization error; `FAKE_AZ_NOLOGIN=1` makes every command ask for `az login`.

## `fake-aws-mcp`: a stand-in for the AWS MCP Server's signing proxy

Speaks MCP over stdio like `uvx mcp-proxy-for-aws`, offers the same tool names, and answers
`aws___run_script` for a few ECS, EC2 and STS calls (an ECS cluster `payments` with services `web`
and `worker`). An `UpdateService` with `desiredCount` is remembered in `$FAKE_AWS_MCP_STATE`.
Start the server with it in place of the real proxy (the real AWS CLI stand-in `fake-aws` is still
needed for the AWS connection test):

```sh
PATH=/tmp/fake-aws/bin:$PATH FAKE_AWS_STATE=/tmp/fake-aws/state.json \
  FORGELINE_AWS_MCP_PROXY="python3 $PWD/scripts/dev/fake-aws-mcp" \
  FORGELINE_TOKEN=a-long-dev-token-123456 ./bin/forgeline server --listen 127.0.0.1:8085 --data-dir /tmp/fl-mcp
```

In the console connect AWS (empty profile, any region), then Infrastructure → AWS → AWS MCP Server → Turn on.

## `fake-k8s`: a stand-in for the Kubernetes API

Serves a read-only cluster with one messy application (a privileged host-networked deployment, a
crash-looping pod, a node that is not ready, a load balancer service) on `127.0.0.1:18090` with the
token `dev-token`. It answers any method other than GET with 405.

```sh
python3 scripts/dev/fake-k8s &
```

In the console: Kubernetes → Add cluster → Token, server `http://127.0.0.1:18090`, token `dev-token`.

## sample-plugin

`sample-plugin/build.sh` builds a tiny MCP server image (`FROM scratch`) and prints its image id.
Its `probe` tool also takes `{"connect": "host:port"}` and asks the proxy in `HTTPS_PROXY` for a tunnel
there (see note 021). Put that id in a manifest under a directory, set `FORGELINE_MARKET_DIR` to it, and install it from
the Marketplace to try plugins for real. `FORGELINE_SAMPLE_PLUGIN_IMAGE=<id> go test ./internal/plugin
-run RealDocker` runs it in a real container and checks what it can and cannot do.
