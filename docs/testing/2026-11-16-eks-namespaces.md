---
title: EKS with namespaces checked, 2026-11-16
---

# The EKS cluster template on real AWS, 2026-11-16

The real check of `aws-eks-cluster@1.0.0` (#145, [note 040](../design/040-any-resource-any-cloud.md) step 5) on the
maintainer's Raspberry Pi Forgeline, with OpenTofu 1.13.9, the Kubernetes provider 3.5 and the Pi's AWS role. Account
numbers are masked. Changes were filed by `claude-agent` and approved with the server token, as the maintainer allows
for Claude's own tests.

## Before

With the maintainer's go-ahead, the Pi role's `forgeline-blueprint-roles` policy gained what the setup guide lists:
`eks.amazonaws.com` in the PassRole list and the four EKS managed policies in the attach list. Two more additions came
out of the test (below) and are now in the guide too.

## Steps

1. A blueprint with one block: the EKS template, namespaces `shop` and `data` (both small), an existing role as
   editor of `shop`, one `t4g.small` node. Its checks say it is built in two approved steps and what it costs.
2. First preview: 12 to add, no Kubernetes resources in it. Approved. The cluster (Kubernetes 1.37, standard support,
   IAM-only sign-in), its roles, log group, network policy add-on and access entry were made; **the node group was
   refused**: `Failed to validate if SLR: AWSServiceRoleForAmazonEKSNodegroup already exists due to missing permissions
   for 'iam:GetRole'`.
3. Second preview, now that the cluster was in the state: 9 to add, the node group and the eight Kubernetes objects.
   Approved. **The namespaces, quotas, default sizes and network policies were created** through the Kubernetes
   provider, signed in with `aws eks get-token` at apply time. The node group was refused again.
4. The fix, approved by the maintainer in two steps: `iam:GetRole` on AWS's own
   `AWSServiceRoleForAmazonEKSNodegroup`. The role did not exist in the account yet, and IAM checks a role that does
   not exist by its bare name, so the statement lists it with and without its path. Third preview: 1 to add. Approved:
   the node group came up (`AL2023_ARM_64_STANDARD`, one node), and EKS made the service role itself.
5. Checked through AWS and the cluster's API:
   - cluster `ACTIVE`, support `STANDARD`, authentication `API`; nodes `ACTIVE`;
   - the editor role has `AmazonEKSEditPolicy` scoped to namespace `shop` only;
   - each namespace has the quota (2 CPUs and 4 GiB requested, 20 pods), the default sizes and the policy
     `own-namespace-only`;
   - a web pod in `shop` got the default requests and limits; **a pod in `shop` reached it; a pod in `data` could
     not**.
6. Removed everything in one approved step (20 removed): the Kubernetes objects first, then the nodes and the cluster.
   The blueprint deleted. AWS then listed no cluster, instance, security group, role or log group for it. What stays
   is AWS's own `AWSServiceRoleForAmazonEKSNodegroup`, which EKS made on first use; it is free and shared by the
   account.

## Found

- A failed first step is not a dead end: the next preview picked up from the state and showed exactly what was left.
- The summary said "Nothing in it is reachable from the internet" beside a public Kubernetes API. Templates now name
  what they expose (`internet_facing`), and the summary repeats it.

## Not checked

- The administrator role input, Spot nodes, x86 node sizes, a pinned Kubernetes version.
- Changing the namespaces of a built cluster (adding or removing one).
- The template form on the canvas with a real blueprint (the fields are of kinds checked before).
