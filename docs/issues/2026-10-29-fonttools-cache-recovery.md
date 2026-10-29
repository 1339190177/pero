---
kind: incident
status: resolved
---

# Recover incomplete cached fontTools environments

## What / why

Windows aggregate CI run `40788131032`, job `123166855114`, restored the
Rust `target` cache and then failed in font preparation: the managed Python
interpreter existed, but `python -m pip` could not import `pip.__main__`.
The bootstrap treated interpreter existence as proof of a complete venv;
it therefore reused an incomplete cached environment. The log establishes
cache restoration and missing pip code, not how that code was lost.

## Repair

Recreate the managed venv with the selected base Python whenever the pinned
fontTools runtime is unusable, before installing the pinned dependency.
Do not use `ensurepip --upgrade` alone: installed distribution metadata can
survive missing package code. Probe the modules consumed by `fonts.py`, not
only the root package's version. A working installed tool stays offline.

## Verification

`node --test scripts/build/fonttools.test.mjs` passes all 8 tests on Linux
using real Python venvs and pip, with an offline local fixture wheel. Cases
cover missing environments, a surviving interpreter with missing
`pip.__main__`, missing fontTools runtime modules, interrupted creation without
pip, healthy offline reuse, installation failure propagation, and platform
interpreter paths. The fixture version is isolated; production stays at
`fonttools==4.60.1`.

Restoring the previous existence-only guard in a temporary copy reproduces
`No module named pip.__main__` in the recovery test. `node --check` passes for
the bootstrap and font build scripts; `git diff --check` passes.

The local checks above were not Windows execution evidence. Final
[CI run 40807109189](https://github.com/phodal/pero/actions/runs/40807109189) passed all 15 jobs on
`78a402d4be2ce89695d40bbd44797d4a174d70c8`, including the actual Windows font-tool
recovery tests, build and Rust aggregate. This closes the incident; no dependency
pin or system security setting was relaxed.
