---
kind: incident
status: monitoring
date: 2026-10-28
---

# Go builder rebuild after clean

After a successful Go build and PostgreSQL acceptance run, the Bazel pilot was
cleaned and rebuilt with a shared disk cache and a restricted client PATH. That
build passed, as did a separate source snapshot reusing the disk cache. A later
PostgreSQL invocation without the disk-cache flag failed while rebuilding the
rules_go `GoToolchainBinaryBuild` tool, before any tests ran. The wrapper exited
with status 1 and no compiler diagnostic.

The same invocation retried with `--sandbox_debug` rebuilt the tool and passed
both PostgreSQL targets. A subsequent `bazel clean` followed by the ordinary
PostgreSQL command rebuilt successfully in 25.81 seconds: all 7 cases passed,
with no skips. No source or toolchain changes were required between failure and
success. The failure has not been reproduced and its cause remains unknown.

Keep this as a local intermittent-build tracker. If it recurs, retain the failed
sandbox and compiler output before changing tools or cache settings. Passing
follow-up builds establish the recorded pilot checks, not universal toolchain
reliability. No automatic retry or relaxed verification was added to the project.
