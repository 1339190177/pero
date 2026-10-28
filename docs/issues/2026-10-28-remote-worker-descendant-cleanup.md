---
kind: incident
status: resolved
---

# Remote Worker cancellation could leave an ACP Agent running

During remote Worker fault injection, cancelling the executor process group alone
did not stop the Agent. ACP SDK 3.5.0 starts the Agent in a separate process group.
Lease loss could therefore stop reporting while descendant code kept running.

The Worker now tracks the executor's descendant process groups, stops them during
cancel/lease-loss cleanup, and checks the remaining group membership. Parent-death
handling also stops orphaned execution. Unconfirmed cleanup produces `reconcile`;
the control plane quarantines the Worker slot and does not replay the Run.

Verification covers silent ACP cancellation, lease expiration, descendant cleanup,
supervisor SIGKILL, restart reconciliation and exactly one prompt. Tests use an ACP
fixture and temporary workspaces; they do not establish adversarial sandboxing or
deployed Linux/Kubernetes acceptance. The checks live in the Worker tests and
`tests/e2e/remote.mjs`.
