---
title: Data pipeline template checked, 2026-11-16
---

# The data pipeline template on real AWS, 2026-11-16

The real check of `aws-data-pipeline@1.0.0` and writer links (#143, [note 040](../design/040-any-resource-any-cloud.md)
step 5) on the maintainer's Raspberry Pi Forgeline, with OpenTofu 1.13.9 and the Pi's AWS role. Account numbers are
masked. Changes were filed by `claude-agent` and approved with the server token, as the maintainer allows for Claude's
own tests.

## Before

With the maintainer's go-ahead, the Pi role's `forgeline-blueprint-roles` policy now may also pass `fl-*` roles to
`firehose.amazonaws.com`, as the setup guide says since #143. One line added, checked by reading the policy back.

## Steps

1. A blueprint: a bucket `lake`; the data pipeline `events` (columns `event` string and `n` bigint, stored every 60
   seconds or 1 MB, at most 1 GB per query) linked to `lake` with read and write; an app running the AWS CLI image with
   no tasks, linked to `events`. Its checks read "app sends to Events (its role may only firehose:PutRecord,
   firehose:PutRecordBatch on it)".
2. Previewed: 22 to add. The guardrails noted only `unchecked-policy` on the template's own role policy (its JSON is
   known only at apply). Sent, approved: 22 added, in one apply.
3. Two one-off tasks of the app's own task definition, so they ran **as the app's role**:
   - `firehose put-record-batch` of three JSON records: `FailedPutCount: 0`.
   - `firehose describe-delivery-stream`, which the template does not grant: `AccessDeniedException` for
     `assumed-role/fl-…-app-task`.
4. About a minute later the bucket held one gzip file under `fl-…-events/year=2026/month=11/day=16/`.
5. Athena, in the pipeline's workgroup: `SELECT event, n, year, month, day FROM fl_…_events.records ORDER BY n`
   returned `signup 1`, `order 2`, `order 3`, all `2026 11 16`, scanning 64 bytes. No crawler ran: the partitions come
   from projection. The result file went to `fl-…-events-athena/` in the same bucket.
6. Removed everything: two approved steps, as for any bucket that holds files (protection off and 18 removed, then the
   last 4). The blueprint deleted. AWS then listed no Firehose stream, Glue database, Athena workgroup, bucket, IAM role,
   log group or cluster for it.

## Not checked

- A Lambda function reshaping records on the way (the module passes `tofu validate` with and without one). It needs a
  function image that answers Firehose's transformation format, which no test image has yet.
- The template form on the canvas with a real blueprint (the Columns field uses the same map field as Lambda's
  Settings, which was checked on 2026-11-14).
