---
title: The resource block checked, 2026-11-17
---

# The resource block on real AWS, 2026-11-17

The real check of the generic resource block (#149, [note 040](../design/040-any-resource-any-cloud.md) step 7) on the
maintainer's Raspberry Pi Forgeline, after the updater deployed it, with OpenTofu 1.13.9 and the Pi's AWS role. The
blueprint was filed by `claude-agent`; the allow list was set and the changes approved with the server token, as the
maintainer allows for Claude's own tests. Account numbers are masked.

## Steps

1. Allowed `aws_sns_topic`, `aws_sns_topic_subscription` and `aws_ssm_parameter` for the primary account. The first
   call made the AWS provider's schema on the Pi in 30 seconds (provider 6.73, 1,766 types) and kept it (9.6 MB).
   `aws_iam_access_key` is answered "never allowed".
2. A blueprint with a setting that holds a secret (`aws_ssm_parameter.value`) was refused on save: "it holds a secret.
   Keep secrets in a secret block".
3. The blueprint: a queue `jobs`; a topic `alerts` (display name `fl-check`); a subscription whose `topic_arn` is
   linked to the topic's `arn` and `endpoint` to the queue's `arn`, protocol `sqs`; and a parameter
   `/forgeline-check/note` whose value is the text `${path.module} and %{ if true }x%{ endif }`. Previewed: 4 to add
   (28 seconds); sent, approved, applied: 4 added.
4. AWS then had the topic with display name `fl-check`, one `sqs` subscription to `fl-…-jobs`, and the parameter with
   exactly that text: OpenTofu did not read it as an expression.
5. The types were taken off the allow list. A build preview was then refused (409, "aws_sns_topic is not allowed in
   this account"); a removal preview was not: 4 to remove, approved, applied. The parameter, topic, subscription and
   queue are gone, the blueprint deleted and the allow list put back as it was (none). The audit log has "primary
   allows …" and "primary no longer allows …".

## Not checked

- A nested block set through the form on a real resource (Route 53 records with an alias are checked against the real
  schema in the tests, and `tofu validate` passes).
- Keep-data and the unprotect step for a bucket, table or database made as a resource block (tested on the generated
  code only).
