---
kind: issue
status: active
date: 2026-10-29
---

# Native release first-run verification

The first multi-OS release is being verified in Actions run 40784807820.
Regular CI run 40784799949 failed on Linux before compiling the UI: Cargo refused
its `--locked` build because the registry-based lock still needed the pinned
local GPUI patch resolution. The release workflow already performs that resolution;
regular Desktop CI did not. This is a build preparation mismatch, not a passing
native test or a released-package result.

The bounded fix adds the same `scripts/cargo.mjs check -p pero-contracts` step
before regular Desktop builds. It retains locked compilation after preparation.
The new CI run and remaining release platforms must be observed before closeout.
