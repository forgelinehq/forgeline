---
title: Step Functions template checked, 2026-11-16
---

# The Step Functions workflow template on real AWS, 2026-11-16

The real check of `aws-step-functions-workflow@1.0.0` ([note 040](../design/040-any-resource-any-cloud.md)) on the
maintainer's Raspberry Pi Forgeline, with OpenTofu 1.13.9 and the Pi's AWS role. Account numbers are masked. Changes
were filed by `claude-agent` and approved with the server token, as the maintainer allows for Claude's own tests.

## Before

The Pi role's `forgeline-blueprint-roles` policy was brought in line with the setup guide (#135): it may hand `fl-*`
roles to ECS, AgentCore, Lambda and Step Functions, with the maintainer's go-ahead.

## Steps

1. A test ECR repository through a proposal (one `CreateRepository` call, read before approving) and an arm64 image of a
   function that appends its `STEP_NAME` setting to the `steps` it receives.
2. A blueprint: two Lambda function template blocks, `step-a` and `step-b`, and a workflow linked to `step-a` and then
   `step-b`.
3. Preview: a real `tofu plan` of 11 resources across three modules; three `unchecked-policy` warnings (the policies are
   only known at apply), no denial. Applied.
4. The first run, started seconds after the apply, failed: the workflow's role was refused `lambda:InvokeFunction`. Its
   policy named exactly the two functions and their versions; the new policy had not reached Step Functions yet (IAM
   is eventually consistent), and the step's retry 2 seconds later was too soon.
5. A second run a minute later succeeded with the output `{"steps": ["start", "step-a", "step-b"]}`: both functions,
   in the order of the links, each one's result passed to the next.

## Removed

One "remove everything" step (11 removed: no tables, so nothing protected), the blueprint deleted, the test ECR
repository deleted with its image. AWS reports no state machine, functions, roles, log groups or repository left.

## Learned

A workflow may refuse its first runs for up to a minute after it is first built, while AWS spreads its new role. The
console tour says so.

## Fargate Spot (#137), same day

A blueprint with one nginx app set to Fargate Spot: the preview added the cluster's capacity providers (10 resources);
after the approved apply AWS reported the task `RUNNING` with capacity provider `FARGATE_SPOT`, and the cluster offering
`FARGATE` and `FARGATE_SPOT`. Removed in one approved step (10 removed), the blueprint deleted; the cluster is
`INACTIVE`.
