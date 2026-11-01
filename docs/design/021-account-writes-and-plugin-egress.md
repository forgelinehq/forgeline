# 021 Per-account writes and plugin egress allowlists

Status: part A (per-account writes) **built**; part B (plugin egress allowlists) **built**. Pictures: [60](../console/60-account-writes.png), [61](../console/61-account-change.png), [62](../console/62-plugin-hosts.png). Follows 017 (platform roadmap), 016 (AWS MCP Server) and 018 (plugins).

## A. Per-account writes

Today a change can be proposed for the primary AWS connection only; added accounts are read-only.

| Decision | Choice |
| --- | --- |
| Opt-in | Each added account has a `writes` switch, **off by default**, that only an administrator can turn on (`PATCH /v1/aws/accounts/{id}`). A developer cannot widen where changes may go. Turning it off, or removing the account, cancels every proposal still waiting for that account |
| Which account | `proposeChange` (cloud changes) and the assistant's `propose_aws_change` take an optional `account` id. Empty means primary, as before |
| One proxy per account | The AWS MCP signing proxy is started per call with that account's profile and region (it already was per call; the settings are now passed in, not read from the primary). The primary's SSO session is never used for another account |
| Binding | A proposal records the account id, its AWS account number (proven with `sts get-caller-identity` when it was prepared), profile and region. Approval re-resolves the account, refuses if it was removed, writes were switched off, or profile, region or account number differ, and (for AI proposals) the hash covers the target, so changing it changes the hash the approver signs |
| Visibility | The approver sees the account name and number on the proposal. Titles and the audit log name the account. A change against account X can never run against Y |
| One at a time | The "one change per target" lock includes the account, so the same stack name in two accounts does not collide, and the same resource in one account still does |
| Unchanged | Four-eyes, expiry, the script checks (no delete/terminate/IAM), previews, environments (primary only), Guard and the explorer |

Not covered: cross-account roles or assume-role chains (an account is a profile on the server), and a
policy that restricts which developers may propose to which account (only the per-account switch).

## B. Plugin network allowlists by host

Today a plugin gets `--network none` or the whole network. Planned: an install grant can name hosts.

| Decision | Choice |
| --- | --- |
| Enforcement | By topology, not by the plugin's cooperation. Each call that has an allowlist gets its own `docker network create --internal` network (no route out) and the container is attached only to it. The only reachable address is the network's gateway, where Forgeline listens for that call with a small forward proxy. A plugin that ignores proxy settings reaches nothing |
| The proxy | HTTP CONNECT and plain HTTP forwarding. It checks the requested host against the grant (exact name or `*.suffix`), allows only listed ports (default 443), resolves the name itself, **refuses loopback, private, link-local, metadata and multicast addresses**, dials the address it resolved (no second lookup), caps concurrent tunnels, bytes and time, and counts allowed and denied hosts into the audit entry for the call |
| Per-call credential | The proxy listens on the network's gateway, an address on the host that other containers (another plugin, anything on the default bridge) can reach too. So every request must carry a random per-call token (`Proxy-Authorization`, Basic or Bearer), compared in constant time; anything else gets 407 and is counted as `unauthorized` in the audit entry, which marks it not ok. The token reaches the plugin only inside the standard proxy variables (`http://forgeline:TOKEN@gateway:port`), through the docker client's environment, never the command line |
| Budget and time | 64 MiB per call in total, shared by every tunnel and transfer and counted for request bodies, bytes sent right behind a CONNECT and responses; hitting it closes the tunnel or aborts the transfer (the client sees a failure, not a short 200) and counts as a denial. A tunnel is idle only when it is quiet in both directions for 30 s; a plain-HTTP response body that stalls for 30 s is aborted and releases its slot (8 at a time) |
| Failures are not refusals | A name that does not resolve, a dial that fails or an upstream that hangs up for an allowed host is counted as `failed`, not `denied`; only policy refusals (not listed, private address, byte budget, missing credential) mark the audit entry not ok |
| End of call | Ending the call closes every tunnel and upstream connection and waits for the handlers before the audit entry is written, so the entry is complete and nothing moves afterwards |
| Cleanup | The network is removed after the container, retried with backoff, and a failure is logged. A background sweeper (at server start when plugins are on, and again every five minutes and after a failed removal) removes leftover `forgeline-plg-*` networks, sparing those of calls in flight. It never runs on the call path |
| No DNS | The container has no resolver that reaches the outside; names are resolved by the proxy only |
| Fail closed | If Forgeline cannot listen on the gateway (for example it runs inside a container itself), the call is refused with that reason. It never falls back to open network |
| Grant | `network` becomes `none`, `hosts` or `open`. `hosts` carries the list. The manifest says which hosts it needs; the administrator grants all, some, or none. A changed host list in an update needs a fresh grant, like a changed image |
| Guarantees | The container can open TCP only to the proxy. The proxy serves only a client holding this call's token, and tunnels only to allowed names on allowed ports and only to public addresses (loopback, private, link-local, CGNAT, reserved blocks, and the IPv6 ranges that embed IPv4: 6to4, Teredo, NAT64, are refused whole). Granted hosts are validated and stored in normal form, so a grant cannot make later calls fail the spec check |
| Does not guarantee | What an allowed host does with data (a CDN or relay host can forward it); TLS is tunnelled, not inspected, so an allowed host name is trusted by name only (domain fronting through an allowed shared host is possible); UDP, ICMP and raw IP are blocked by topology but not logged; a host kernel or Docker flaw still escapes everything; the per-call token is on the container's environment, so a plugin can hand it to something else on the same private network (nothing else is there) but it dies with the call; a local user who can read the docker client's environment on the host (same user or root) can read it; the sweeper treats every `forgeline-plg-*` network on the daemon as its own, so two Forgeline servers sharing one daemon would remove each other's idle networks |

## Build plan

1. Part A, one pull request: `writes` switch, account on proposals, per-account proxy, binding and
   invalidation, UI, tests (including mutating each rule).
2. Part B, one pull request: grant model, proxy package, per-call network lifecycle, tests against a
   real Docker daemon, UI.

## What was built, and how it was checked

Part A: see the pull request that built it; each rule above has a test that fails when the rule is broken
(about 40 mutations).

Part B: `internal/plugin/egress.go` (the proxy), `Runner` (the per-call network, the proxy's address in
`HTTP(S)_PROXY` with the per-call credential, cleanup with retries, a background sweeper for leftover `forgeline-plg-*` networks), host grants in the
marketplace (a package asks for hosts or for the whole network, not both; the administrator ticks a subset; a
changed ask needs a fresh grant), and `plugin.egress` audit entries.

- Checked against a real Docker daemon (29.3): a container on an `--internal` network reaches the gateway
  and nothing else (external IPs are unreachable, external DNS answers SERVFAIL); the sample plugin tunnelled to
  a listed host, was refused for an unlisted host, an unlisted port and an IP address, and no network or container
  was left afterwards. With the production configuration (system resolver, no test hooks) a public host on the
  list worked, and a listed public name that resolves to 127.0.0.1 was refused.
- A high-effort code review found ten problems (per-direction idle timeout, no proxy authentication, unvalidated
  and unnormalised grants, upstream failures counted as denials, tunnels outliving the call, best-effort cleanup
  and a sweep on the call path, an unenforced and silent byte budget, missing IPv6 transition and reserved
  ranges, a stalled body holding a slot). All are fixed, each with a test that fails when the fix is removed.
- 40 rules were mutated (address ranges, the one-bad-answer rule, dialling the resolved address, wildcards,
  ports, limits, cleanup, the fail-closed path, the proxy environment); each break fails a test.
- Not verified: Docker Desktop and rootless Docker (their bridge and gateway handling differs; a failed listen
  refuses the call), IPv6-only hosts, a server running inside a container.
