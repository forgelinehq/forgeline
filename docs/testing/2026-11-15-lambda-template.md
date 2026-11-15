---
title: Lambda template checked, 2026-11-15
---

# The Lambda function template on real AWS, 2026-11-14 to 15

The real check for the template format ([note 040](../design/040-any-resource-any-cloud.md), step 2) on the
maintainer's Raspberry Pi Forgeline, with OpenTofu 1.13.9 and the Pi's AWS role (IAM Roles Anywhere). Account numbers
are masked. Changes were filed by the `claude-agent` user and approved with the server token, as the maintainer allows
for Claude's own tests.

## Steps

1. A test ECR repository, created through a proposal the assistant filed (one `CreateRepository` call, read before
   approving), and an arm64 Lambda image (Python 3.13 base) that writes each queue message into the table named by its
   `TABLE_ORDERS` setting, built and pushed from the Pi.
2. A blueprint: a queue that triggers an `aws-lambda-function@1.0.0` template block (256 MB, `MODE=check`), which reads
   and writes a table.
3. Preview: a real `tofu plan` through the module, 7 resources to add (no schedule resources, since none was set), one
   guardrail warning (`unchecked-policy`: the function's policy is only known at apply), no denial.
4. Apply: **failed** creating the function. AWS refused `iam:PassRole` for `role/fl-663d58-fn`: the Pi role's
   `forgeline-blueprint-roles` policy let it pass `fl-*` roles only to ECS and AgentCore. With the maintainer's
   go-ahead, `lambda.amazonaws.com` was added to that one condition. Forgeline had kept the state of the failed apply.
5. A new preview built on that state planned only the 2 missing resources (the function and its queue trigger); applied.
6. A message sent to the queue was in the table within seconds, under its message id; the function is arm64 with
   256 MB, its settings hold `MODE` and the linked table's name, and its log says `{"records": 1, "source": "queue"}`.

## Removed

Through the blueprint's "remove everything": step 1 (1 changed: the table's deletion protection off; 6 removed),
step 2 (the table removed); then the blueprint was deleted and the test ECR repository with its image. AWS reports no
queue, table, function, role, log group or repository left.

## Kept

The Pi role may now pass `fl-*` roles to Lambda as well as ECS and AgentCore (needed for any Lambda template).
