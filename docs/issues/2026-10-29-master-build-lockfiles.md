---
kind: issue
status: verifying
date: 2026-10-29
---

# Master build lockfiles

At master `8b55dd835587cc8c9fdbadba2eae3548f45fd032`, CI run
[40837327389](https://github.com/phodal/pero/actions/runs/40837327389) failed
the Linux and macOS frozen Bazel checks. Pages run
[40837327403](https://github.com/phodal/pero/actions/runs/40837327403) failed
before compiling the release workbench; its deployment job did not run.
Both failures were first diagnosed at `9549cfb`. The repair was then rebased
and revalidated against `8b55dd8`, preserving its concurrent local-worker work.
These failures are separate from the unmerged DSH work in PR #4.

## Causes and bounded correction

- `3002f03` added the composer and sidebar scripts to `package.json` after
  `bd15dd7` refreshed the rules_js input cache. Later local-worker scripts also
  changed that input. Script-only changes still invalidate the cache.
  Regenerate it with the pinned Bazel/pnpm toolchain;
  npm dependencies and the translated pnpm lock do not need upgrades.
- `scripts/lib/cargo.mjs` patches five GPUI packages to the external checkout
  pinned by the workflows at `ec89cfc36404667b7947aeb3d40b1c2859d729dd`.
  `Cargo.lock` instead identified those packages as registry crates. Cargo
  correctly refused to change their identities under `--locked`. Desktop CI's
  preceding unlocked contracts check masked the mismatch; Pages begins with
  the locked release build. Let Cargo resolve the existing pins and commit the
  path-package identities rather than add another unlocked build preparation.

The correction retains all dependency versions, the exact GPUI checkout,
`ASPECT_RULES_JS_FROZEN_PNPM_LOCK`, and the Wasm build's `--locked` argument.
No workflow, upload, or deployment configuration is changed.

## Verification

Both original lock failures were reproduced before regeneration: frozen Bazel
exited 48 and Cargo exited 101. On Linux after regeneration:

- `bazel run @npm//:sync` succeeded with the pinned Bazel 9.2.0 / pnpm 10.28.2;
  only the watched `package.json` hash changed. `pnpm-lock.yaml`,
  `package-lock.json`, and `MODULE.bazel.lock` are unchanged.
- `bazel test --config=ci //:checks`: all seven checks passed, without rewriting
  the committed locks. `bazel build //server/cmd/server` also passed.
- Full Cargo metadata resolution (without `--no-deps`) passed under `--locked`
  with stable and `nightly-2026-10-28`. All 844 package names, versions, and
  dependency lists are unchanged; only the five GPUI registry source/checksum
  pairs are removed.
- `npm test`: 73 Rust tests passed, with none failed or ignored. The same crate
  suites also passed with explicit `--locked`. `npm run test:contracts` passed;
  that existing suite exercises 30 of 45 declared Host event kinds.
- Go 1.26.4 `go -C server test -race ./...` passed. Database and external-service
  opt-in scenarios were not configured; this is not database acceptance.
- Under Node 22.23.4, `PERO_BUILD_RELEASE=1 npm run build:wasm` passed,
  including the pinned nightly `--locked --release` build, wasm-bindgen, and
  Binaryen 132 optimization. `npm run build:web` and `npm run check` passed.
- A local Pages directory assembled with the workflow's exclusions passed all
  original CNAME, required-file, Wasm-presence, and relative-asset checks. The
  optimized Wasm is 15,078,605 bytes, SHA-256
  `bbc3d35018910673d5d033b77130e119029815b401dde382f32871098607f9c4`.
  This Linux container used Python file copying in place of unavailable rsync;
  it did not perform the Pages service configuration check or artifact upload.

The final commands left both corrected lockfiles unchanged. The initial local
tool bootstrap required writable cache directories and the system Java trust
store; no repository gate or TLS verification was disabled.

Remote CI and Pages acceptance remain pending publication of the repair.
Local build verification does not upload artifacts or deploy the site.
