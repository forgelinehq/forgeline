# 043: Native image builds

Status: **decided 2026-11-12 (maintainer); slice 1 (a `build` step, pushes to allowed ECR repositories) built 2026-11-13.**
Builds on note 005 (threat model), note 011 (workflow steps run by the server) and note 035 (blueprints make ECR
repositories with a GitHub push role).

## Question

Today an image that a blueprint deploys is built by GitHub Actions: Forgeline suggests a workflow, makes an ECR
repository and a role GitHub may take, and deploys what lands there. Can Forgeline build the image itself, so a team
does not need Actions, without letting a pipeline file push whatever it likes with the server's cloud credentials?

## Decisions

1. **A `build` step, run by the server.** Like a `workflow` step it is not a container the pipeline chooses: the
   server runs BuildKit through the host's Docker (`docker buildx build`) on a directory of the run's workspace. The
   step names a `context` and `dockerfile` inside the workspace, an optional `platform`, build `args` (plain values,
   never secrets), a `network` (`egress` by default, or `none`), and optionally a `push` target and `tags`. It has no
   image, script, environment or secrets of its own.
2. **What runs during a build.** The Dockerfile's `RUN` lines run in BuildKit's own containers: no host mounts beyond
   the context, no secrets, no entitlements (`--allow` is never passed, so no `security.insecure` and no host
   network), and no network at all with `network: none`. This is no more than a pipeline can already do by running a
   container step, which is why any run may build.
3. **Who may push: trust is decided by where the run came from.** A push needs all of:
   - the run was started by GitHub for a **push** (`refs/heads/<branch>`), never a pull request, so its pipeline file and
     Dockerfile are code someone with write access put on that branch;
   - an administrator's **push rule** names the source repository, the branch (patterns as on the GitHub allow list)
     and the exact target repository (`<account>.dkr.ecr.<region>.amazonaws.com/<name>`), and which Forgeline AWS
     account pushes there;
   - the step's `push` equals the rule's target. Anything else fails before the build starts, with the reason.
   Runs started from the console have no source checkout yet, so they can build only what is in their workspace and
   cannot push (slice 2 adds a checkout and decides who may push from the console).
4. **Short-lived push credentials that never enter a container.** For each push the server asks the rule's AWS account
   for an ECR authorization token (valid 12 hours; AWS does not offer shorter), writes it to a fresh Docker client
   configuration directory readable only by the server, passes it to `docker buildx` for that one build, and deletes it
   when the build ends. The build's own containers never see it; it never reaches the log.
5. **Pushing is not deploying.** An image in a repository changes nothing that runs. Deploying it still goes through a
   proposal and an approval (note 006), so this adds no path that changes the cloud directly.
6. **Audited.** Every build is in the audit log (repository, commit, context, target, tags); every push with the digest
   it produced; every refusal with its reason.
7. **Local first.** `forgeline run` on a laptop runs the same `build` step through the laptop's Docker and never
   pushes.

## Not in this slice

- Source checkout for console and scheduled runs (slice 2), and builds the assistant or an agent start.
- Registries other than ECR; multi-platform builds in one step; build caches kept between runs; signing and SBOMs
  (milestone M4).
- Rootless BuildKit in its own container (for servers without a Docker daemon, or Kubernetes): it needs seccomp and
  AppArmor turned off for the container, which is a weaker sandbox than the host daemon's BuildKit with no
  entitlements. It comes with the Kubernetes runner.
- Choosing native builds for a blueprint's app block instead of the suggested GitHub workflow (slice 3).

## Checks

- Tests: the step's validation; the trust rules (pull request, console run, unlisted branch, unlisted target, a target
  that differs by one character: all refused before anything runs); the token written only to a private directory that
  is gone afterwards and never in the arguments or the log; the arguments never carrying `--allow` or a host mount.
  Security checks are mutation-tested.
- On the Pi: a real repository pushed to `main` builds and pushes to a real ECR repository through a push rule; a pull
  request on the same repository builds and does not push. The test repository and image are removed afterwards.
