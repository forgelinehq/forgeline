# 046: Exporting agents: a bundle, Docker Agent YAML, an OCI artifact, an A2A card

Status: **accepted 2026-11-19: every decision as proposed** (end of this note). **Step 1 is built**: both downloads, with
their schema, tests, and an audit entry. Not yet checked with the real `docker agent run`. Revises note 045, decision 3,
which said an agent is not exported. On 2026-11-18 the maintainer asked for agents to be exportable too, in open formats.

## Question

An agent in Forgeline is a pinned version: instructions, a model, skills at fixed versions, tools and a tier
(note 024, 045). How can a person take one out of Forgeline, run it somewhere else, or let other agents find and call
it, without carrying keys out or weakening what Forgeline promises (writes only through proposals that a different
administrator approves, nothing above the starter's role, everything audited)?

## The formats, as they stand in November 2026

| Format | What it is | Fits for |
| --- | --- | --- |
| **Agent Skills** ([agentskills.io](https://agentskills.io/specification)) | A folder per skill with a `SKILL.md`: YAML front matter (`name`, `description`, optional `license`, `compatibility`, `metadata`, `allowed-tools`) and Markdown. Published by Anthropic in December 2025, now with the Agentic AI Foundation; read by Claude Code, Copilot, Cursor, VS Code and many more | Forgeline's skills, as they are |
| **Docker Agent YAML** ([docs](https://docs.docker.com/ai/docker-agent/reference/config/)) | One file (`version: 16`): `agents` (each with `model`, `description`, `instruction`, `toolsets`, `sub_agents`), `models`, `mcps`, `providers`, `metadata`. Models as `provider/model`; keys from environment variables (`ANTHROPIC_API_KEY`) or `${env.VAR}`. MCP servers as a remote `url` or a local `command` | A runnable copy of the agent outside Forgeline |
| **OCI artifact** | `docker agent share push ./agent.yaml ghcr.io/org/agent:v1` stores the YAML as one layer with `io.docker.agent.*` annotations; `docker agent run <ref>` runs it | Sharing an agent through a registry, like an image |
| **A2A agent card** ([spec 1.0](https://a2a-protocol.org/latest/specification/)) | JSON at `/.well-known/agent-card.json`: `name`, `description`, `version`, `supportedInterfaces` (each `url`, `protocolBinding` such as `JSONRPC`, `protocolVersion` `1.0`), `capabilities`, `defaultInputModes`, `defaultOutputModes`, `skills` (`id`, `name`, `description`, `tags`), and `securitySchemes`. Optional signed cards (JWS) and an extended card for signed-in callers | Other agents finding and calling a published Forgeline agent |
| A portable "agent.yaml" | No single standard: Open Agent Specification, IETF drafts (ADL, AAIF, FAFA), and tools that use the file name. None is settled | Not chosen; Forgeline's own file instead (below) |

Checked since (Docker Agent's skills and providers pages): Docker Agent reads `SKILL.md` folders from `.agents/skills/`
and other standard folders, and also takes **inline skills** in the YAML (`name`, `description`, `instructions`). Its
providers include `anthropic`, `openai`, `google`, `amazon-bedrock` and `azure`; an OpenAI-compatible server is a
`providers:` entry with `base_url` and `token_key` (the name of the environment variable that holds its key). Remote MCP
servers take `remote.url`, `remote.transport_type` (`streamable` or `sse`) and `remote.headers`, whose values may be
`${env.NAME}`. `docker agent run <file> --dry-run` loads a file without running it. Still to check: the A2A 1.0 field
tables in full (step 4).

## Decisions (proposed)

### 1. What is exported

- **One fixed version** of an agent, never "the latest". The export names the agent, the version and its fingerprint
  (the hash Forgeline already keeps), so a copy can always be traced back.
- **Never a secret.** Model keys become the provider's usual environment variable (`ANTHROPIC_API_KEY`); an MCP
  server's sign-in becomes `${env.NAME}` with the name shown; nothing from the vault is read.
- **Who can:** downloading is for developers and up (they can already read the instructions, skills and tools);
  pushing to a registry and publishing an A2A card are for administrators (decisions 4 and 5).
- **Audited:** every export, push and card says who, which agent, which version and which format.

### 2. Two downloads: a Forgeline bundle and Docker Agent YAML

**Download** on an agent's page offers:

1. **Forgeline bundle** (`<agent>-v<n>.zip`):
   - `agent.yaml`: Forgeline's own description of the version: name, description, instructions, model, tier, tools,
     the skills by name and version, and the fingerprint. Made from the Go types, with a JSON Schema in `schema/` like
     the pipeline file (principle 1), so it is checked when it is read back.
   - `skills/<name>/SKILL.md`: each pinned skill in the Agent Skills format, with `name`, `description` and
     `metadata: {forgeline-version: "3"}`. Any tool that reads Agent Skills can use them on their own.
   - `README.md`: what is in it, what was left out and why (decision 3).
   This is the format for moving an agent between Forgeline servers (decision 6) and for keeping it in Git.
2. **Docker Agent YAML** (`agent.yaml`, `version: 16`): one `root` agent with the instructions, the skills inline (so
   the one file is the whole agent, which is all an OCI push carries), `model` as `provider/model` mapped from the
   Forgeline provider's kind (Anthropic, OpenAI, Gemini as `google`, Bedrock as `amazon-bedrock`, Azure OpenAI as
   `azure` with its `base_url`, OpenAI-compatible as a `providers:` entry with `base_url` and `token_key`), and each
   remote MCP server as an `mcps:` entry with its URL, only the tools the agent has, and its sign-in header as
   `${env.NAME}`. `metadata` carries the description, the version and the fingerprint.

### 3. Forgeline's own tools outside Forgeline

Most of an agent's tools are Forgeline's (`list_runs`, `aws_resources`, `propose_aws_change`, …). They have no meaning
without the server, and they must never run outside its checks. Two ways, both proposed:

- **Left out (the default).** The download lists them in its README as "needs Forgeline" and leaves them out. The copy
  can still answer and use the remote MCP servers. Plugin tools and local MCP servers are left out the same way.
- **Through Forgeline, when publishing exists (note 045, decision 4).** The export adds Forgeline's MCP server
  (note 020) as one remote MCP toolset, with an **agent key** scoped to that agent and that version. Every call still
  goes through the server: the key's role and limits, the audit, and proposals that a different administrator approves.
  This is the only way an exported agent can propose a change. Not before agent keys are built.

So an exported copy can never do more than read on its own, and anything it changes still goes through approval.

### 4. Pushing to a registry as an OCI artifact

- **Push…** sends the Docker Agent YAML to a registry an administrator added under Settings (GHCR, Docker Hub, ECR
  through a connected AWS account, or any OCI registry), as Docker Agent stores it: one layer with the
  `io.docker.agent.*` annotations, tagged `v<n>`, so `docker agent run <ref>` works. The bundle can go as a second
  artifact type (its zip as one layer) for Forgeline-to-Forgeline moves.
- Written in Go against the OCI distribution API (upload blobs, then the manifest), with no new dependency.
- Registry credentials are kept in the vault, like every other credential.
- **A push is a proposal** that a different administrator approves, because it publishes outside Forgeline and, for
  ECR, changes the cloud (note 006). The proposal shows the exact files and the target reference.

### 5. An A2A agent card for a published agent

- When an agent is published (note 045, decision 4), its page can also **list it for A2A**. Forgeline then serves its
  card at `/a2a/agents/<id>/.well-known/agent-card.json` and one JSON-RPC interface at `/a2a/agents/<id>/v1`.
- The card: `name`, `description`, `version` (the live version), `supportedInterfaces` (that URL, `JSONRPC`, `1.0`),
  `capabilities` (no streaming or push at first), `text/plain` in and out, one A2A skill per Forgeline skill (its name
  and description, and its tags) or one for the agent as a whole when it has none, and a bearer `securitySchemes`
  entry: calls need an agent key (decision 3), so the card is public but the agent is not.
- Calls run the live version as the key's role, inside its limits, audited, as the API does.
- Signed cards and the extended card come later.

### 6. Bringing a bundle back

**Import an agent** on the Agents page takes a Forgeline bundle, checks `agent.yaml` against its schema, and saves a
new agent (or a new version of one with the same name) through the same checks as the builder. Skills are matched by
their text; a skill that does not exist yet is created. Tools this server does not have are named and dropped. Docker
Agent YAML can be imported later the same way.

## Order of work

1. The two downloads (decisions 1 to 3, tools left out) and their schema.
2. Import of a bundle (decision 6).
3. Registry settings and Push as a proposal (decision 4).
4. With publishing (note 045, step 4): the Forgeline toolset with agent keys (decision 3) and the A2A card (decision 5).

Each step is checked with the real tools before the next: `docker agent run` on the YAML and on the pushed reference,
a skill folder read by Claude Code, and the card fetched by an A2A client.

## Decisions for the maintainer

All five were made **as proposed** (maintainer, 2026-11-19):

1. ~~Formats first~~: **the Forgeline bundle and Docker Agent YAML**, both as downloads.
2. ~~Forgeline's tools in a copy~~: **left out by default**; later through Forgeline's MCP server with an agent key.
3. ~~Push~~: **a proposal that a different administrator approves.**
4. ~~A2A~~: **only for published agents**, one card per agent under `/a2a/agents/<id>/`, JSON-RPC first.
5. ~~Import~~: **now**, as step 2.
