# 026: Access policy for integration tools

Status: **Decided 2026-11-02; slice 1 (connection ceilings and the effective-access view) is built. Needs approval for reads is not.**
Builds on notes 025 (integrations hub), 013 and 015 (the assistant's lookups), 017 (agents and their tool
allowlists) and 006 (proposals). Facts about the code are from `origin/main` at `67d719d`.

## Question

An agent can already only use the tools on its own allowlist, and every change is a proposal. What is missing is a
place where an administrator can say, for a whole connection, "nothing may use this tool", and see in one view
what each agent can actually do. How do we add that without weakening any existing rule or adding a second way
to reach the cloud?

## What exists today

- `assistantLookups()` is the full list of tools the assistant and agents can call. `runLookup` runs one as the
  person who asked, checks the person's role and the running agent's allowlist (`agentScope.allows`), and refuses
  anything else. `lookupHelp` shows the model only the tools it may use.
- No tool writes. The one that asks for a change, `propose_aws_change`, files a proposal that a different
  administrator approves. `request_environment` files a request the same way.
- Refusals are audited (`assistant.lookup`, allowed false).

## Decisions

| Decision | Choice | Why |
| --- | --- | --- |
| What a policy is | A **ceiling per connection**: each integration tool is **Allow** or **Blocked**. No entry means Allow, which is today's behaviour | The smallest thing that gives an administrator a switch, and nothing changes until someone uses it |
| Strictness | **Blocked beats everything.** A tool is usable only if the person's role, the agent's allowlist and the ceiling all allow it | Same rule as the stricter-wins model in note 025 |
| Writes | A tool that asks for a change can never be Allow: it stays a proposal that a different administrator approves. It can be **Blocked**, which stops the tool being used at all | The one rule of this platform (note 006) |
| Needs approval for a read | **Not in slice 1.** It needs a person present to approve each call, which means an approval card in the assistant's event stream (note 024) and a refusal in unattended runs. Designed below, built later | Better absent than faked |
| Which tools | Only tools that reach an integration: `aws_*` and `call_aws` (AWS, or the AWS MCP Server for the ones that need it), `k8s_*` (Kubernetes). Forgeline's own tools (runs, pipelines, audit) are not integration tools and have no policy | A policy belongs to a connection |
| Who changes it | Administrators only. Each change is audited with before and after | Like every other integration setting |
| Failing safely | If the policy cannot be read, integration tools are refused with that reason. A missing policy means Allow; an unreadable one does not | Fail closed on error, open on "never set" |
| Where it is enforced | In `runLookup`, before the tool runs, so a blocked tool never reaches the CLI or the proxy. `lookupHelp` hides it so the model does not try. A refusal is audited against the integration, so its Activity tab shows it | One place, the one all lookups already pass through |
| Effective access | A view per integration: for each agent, each tool is Allowed, or Blocked by the connection ceiling, or Not in the agent's allowlist, or Needs a higher role. Computed on the server so it can be tested | The point of the policy is being able to see the answer |

## Not decided here

- **Needs approval for reads (slice 2).** The assistant stream gets a `forgeline.approval` event; the card shows the
  call and Approve or Deny; the run waits for the decision. In an agent or workflow run, where nobody is
  present, such a tool is refused with "needs a person".
- Per-agent grants beyond the agent's existing tool allowlist, per-role grants, budgets, and a policy for plugin tools
  (plugins already declare their tools and hosts).
- Google Cloud and Azure tools. The assistant does not have lookups for them yet; when it does they join this
  policy, and a write tool for either stays a proposal.

## Tests to write

Go, with the race detector, through the real server: a blocked tool is refused for the assistant and for an agent,
and never reaches the command (the stub sees no call); a blocked tool is absent from `lookupHelp`; a write tool
cannot be set to Allow but can be blocked; an unknown tool or a tool of another integration is refused; only
administrators change it; each change is audited with before and after; a refusal is audited against the
integration and appears in its Activity; an unreadable policy fails closed while a missing one allows; the
effective-access view gives the right reason for each case. Then remove each rule in turn and confirm a test fails.

## What was checked, and what was not

Checked: the facts above, by reading the code; the policy through the real server with a stub for the command-line tools, and in a browser against stand-ins. The browser showed two defects the tests had not (the page sent the proposal tool as "allow", which the server rightly refuses, and a blocked proposal tool could not be put back), both fixed with tests. Also found by reading rather than running: `propose_cloud_change` is called by the MCP endpoint directly, not through `runLookup`, so it has its own gate, as a second layer behind the list of offered tools. Not checked: how the policy behaves under load, and any behaviour of
the real AWS MCP Server when a tool is blocked mid-conversation (the model sees a refusal like any other).
