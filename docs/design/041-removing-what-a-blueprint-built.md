# 041: Removing what a blueprint built

Status: **built (2026-11-12).** Extends notes 035 (blueprints), 039 (more blocks) and 040 (guardrails).

## Question

A blueprint can build things in AWS, but nothing could take them away again. People testing a design want to build it,
look, and remove it so it stops costing money. Removal deletes things, so it needs at least the care a build gets.
How should it work, and what happens to what holds data?

## Decisions

1. **Removal is a preview like any other.** Forgeline plans different code for the same blueprint and state, so a
   removal goes through the same `tofu plan`, the same guardrails, the same approval by a different administrator and
   the same apply of exactly that plan. No `tofu destroy`, no second path. Removal rows make the change high risk, so
   approval rules for high-risk changes (note 032) apply.
2. **Two choices.**
   - **Keep the data** (the default): the code keeps only tables, buckets, databases and secrets, so the plan removes
     the rest and those stay, still protected and still managed by the blueprint. A cache is not kept: what it holds can
     be rebuilt and keeping it keeps a bill.
   - **Remove everything**: AWS refuses to delete a database or table with deletion protection, and OpenTofu refuses to
     plan the removal of a resource marked `prevent_destroy` while it is in the code. So it takes two approvals when
     something is protected. Step 1 plans the data blocks without their protection (and lets a bucket be deleted with its
     files) and removes everything else. Step 2 plans no blocks at all and removes what is left. Forgeline reads the saved
     state to decide which step comes next, so the person presses the same button twice.
3. **A final snapshot of each database** is kept unless the person unticks it. It costs a little storage until someone
   deletes it in AWS; that is said where the choice is made.
4. **The blueprint is kept.** After everything is removed the state is kept (it records how many applies there were, so an
   old approved plan can never apply) and marked empty; the canvas shows **Removed**, and the blueprint can be built again.

## Checked

- Go tests: the removal code per stage (`internal/blueprint/removal_test.go`), and the whole flow on the server with a
  stand-in OpenTofu that keeps attributes in its state (`TestBlueprintRemovalKeepsDataThenRemovesEverythingInTwoSteps`).
  Breaking the stage choice or the keep-data code makes it fail.
- `tofu validate` (OpenTofu 1.13.9, AWS provider 6.72) passes for all three stages' code.
- A browser walk on a dev server with `fake-aws` and `fake-tofu`: build, keep data, step 1, step 2, **Removed**.

- **Real AWS, on the Pi (2026-11-12)**, OpenTofu 1.13.9 and the AWS provider 6.7x, each step approved by a second
  principal:
  - the 2026-11-08 test blueprint (a queue and a log group): one step, 2 removed;
  - a round trip: a queue, a protected table, a protected versioned bucket with a file in it, and a log group were built
    (8 resources); step 1 turned the table's deletion protection off and let the bucket be deleted with its files (2
    changed) and removed the queue and logs (2 removed); step 2 removed the table and the bucket (6 removed). AWS then
    listed none of them.
- Deleting a blueprint is allowed once nothing it built is left; its state is kept so an old approved plan can never
  apply.

## Not checked

- Removing a database (RDS) for real, with and without a final snapshot: it costs money to build one to test it.
