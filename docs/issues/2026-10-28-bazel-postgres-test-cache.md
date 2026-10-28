---
kind: incident
status: resolved
date: 2026-10-28
---

# PostgreSQL acceptance reused local test state

Repeating the same database acceptance invocation reported `Executed 0 out of 2
tests` despite `local`, `no-cache` and `no-remote` tags. Those tags prevent action
result cache reuse but do not force Bazel to rerun a test whose local test status
is already successful. An unavailable or changed external database could therefore
be hidden by a previous passing run.

Both PostgreSQL package tests now use Bazel's `external` test tag to require
execution on every invocation. The existing remote-cache and execution limits
remain. See [Bazel test tags](https://bazel.build/reference/be/common-definitions#common.tags).

Verification on 2026-10-28: two consecutive invocations each executed both test
targets, with 7 individual cases passing and no skips. After stopping the same
disposable PostgreSQL server, the unchanged invocation executed both targets and
failed with connection-refused errors. Removing the database URL also failed.
Local test status therefore cannot conceal the missing external dependency.
