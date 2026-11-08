---
title: Get started
nav_order: 2
---

# Get started

## What you need

- Go 1.27 or newer, `git` and `make` (to build), and Docker, running (to run steps). Linux, macOS, or Windows with WSL2.
- A browser.

No cloud account, database or other setup is needed for this page. Checked from a fresh clone on 2026-11-08.

## 1. Build and start

```sh
git clone https://github.com/forgelinehq/forgeline && cd forgeline
make run-local
```

`make run-local` builds the binary into `./bin/forgeline` (the first build downloads Go modules and takes
under a minute) and starts the server on `127.0.0.1:8080`. On a loopback address with no
`FORGELINE_TOKEN` set, it makes a random token for this run and **prints** two lines:

```text
  Open:    http://127.0.0.1:8080/#token=…
  CLI:     export FORGELINE_TOKEN=… FORGELINE_SERVER=http://127.0.0.1:8080
```

Keep that terminal open. The **Open** link is for step 2; the **CLI** line is for the command line below.

Port 8080 already in use? Start it on another port instead:

```sh
make build
./bin/forgeline server --data-dir .forgeline-server --listen 127.0.0.1:9000
```

To keep the same token across restarts, and rebuild and restart in one go:

```sh
scripts/dev/demo.sh                 # data in /tmp/fl-demo, http://127.0.0.1:8080
scripts/dev/demo.sh ~/forgeline-data 127.0.0.1:9000
```

The token is saved in `<data-dir>/.token` (mode 0600). Running the script again restarts only the server it
started for that data directory; any other Forgeline you run is left alone.

## 2. Create the first administrator

Open the **Open** link from step 1; it fills in the server token for you. (Or open
<http://127.0.0.1:8080> and paste the token.) The first visit shows the setup form: choose a username and a
password of at least 12 characters, and press **Create administrator**.

![First run]({{ '/console/01-setup.png' | relative_url }})

The token is required so that whoever reaches the port first cannot take the server over.
After setup you sign in with the username and password; the token keeps working for the CLI and the API.

Home then shows **Set up Forgeline**: seven steps (your team, GitHub, a cloud account, the assistant, what the AI may
touch, a first pipeline or blueprint), each with a button that opens the page that does it. A step ticks itself when what
it sets up exists, and an administrator can skip any of them. The same list is in **Settings › Setup** later.

![Set up Forgeline on Home]({{ '/console/117-setup.png' | relative_url }})

Prefer a terminal? From the `forgeline` folder:

```sh
FORGELINE_TOKEN=<the server token> ./bin/forgeline onboard --server http://127.0.0.1:8080
```

It shows the same steps and asks what a terminal can answer: which assistant to use (the `claude` program on the server)
and which AWS profile and region to use. API keys and tokens are only ever entered in the console. Add `--non-interactive`
to just print the list (exit code 0 when everything is done or skipped).

## 3. Run a pipeline

Go to **Ship → Pipelines → New pipeline** (or press **New pipeline** on Home) and paste:

```yaml
version: v1alpha1
name: hello
steps:
  - id: greet
    image: alpine:3.24
    run: echo "hello from Forgeline"
```

The editor checks it as you type and says **Valid**. Press **Create & run** (or **Create** to save
without running). You land on the run: the steps as a graph, coloured by status, and the live log, which ends
with `hello from Forgeline`. The first run downloads the `alpine` image, so it can take a few seconds more.
**Run again** runs it once more.

![A run in progress]({{ '/console/05-run-live.png' | relative_url }})

Prefer the command line? From the `forgeline` folder, in a second terminal:

```sh
./bin/forgeline run examples/hello/pipeline.yaml      # runs here, in Docker, with no server
export FORGELINE_TOKEN=… FORGELINE_SERVER=http://127.0.0.1:8080   # paste the CLI line from step 1
./bin/forgeline submit --wait examples/hello/pipeline.yaml        # runs it on the server; it shows in the console
./bin/forgeline schema                                # the JSON Schema, for editor support
```

`run` prints one line per step (`ran`, or `cached` the second time). `submit` needs the token: without the
`export` line it stops with `set --server (or FORGELINE_SERVER) and FORGELINE_TOKEN`. To type `forgeline`
instead of `./bin/forgeline`, copy the binary into a folder on your `PATH`.

## 4. Next

Follow **Set up Forgeline** on Home, or go straight to:

- Put values a step needs in **Secrets**, then ask for them with `permissions.secrets`: [Pipelines]({{ '/guides/pipelines/' | relative_url }}).
- [Connect GitHub]({{ '/guides/connect-github/' | relative_url }}) so a push runs your pipeline.
- [Connect AWS]({{ '/guides/connect-aws/' | relative_url }}) to see and change your cloud.
- Turn on the [assistant]({{ '/guides/assistant-and-agents/' | relative_url }}).

## Server settings

| Variable | Meaning |
| --- | --- |
| `FORGELINE_TOKEN` | Bearer token for the CLI and API, at least 16 characters. Required unless the address is loopback. |
| `FORGELINE_DATABASE_URL` | Postgres URL. Without it, runs live in memory and accounts, pipelines and settings are kept in `<data-dir>/console.json`. |
| `FORGELINE_MASTER_KEY` | 32 random bytes, base64 (`head -c 32 /dev/urandom \| base64`), that encrypt stored secrets. Without it a key file is created in the data directory. |
| `FORGELINE_SECRET_<NAME>` | A secret steps can request without using the vault. |

Flags: `--listen`, `--data-dir`, `--max-runs`, `--parallel`, `--public-url`, `--github-allow`.
Run `forgeline server -h` for the full text.

{: .important }
The server speaks plain HTTP. Put it behind TLS (a reverse proxy or a tunnel) before exposing it, and run it with an AWS role that can do only what you want the console to be able to do.
