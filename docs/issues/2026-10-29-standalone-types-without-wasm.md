---
kind: issue
status: resolved
date: 2026-10-29
---

# Standalone release type check requires unrelated Wasm output

Release run 40841125534 for `v0.1.0-dev.20261029.4` built the Virtual Cell
macOS package, then failed `npm run check` with TS2307 in `web/src/bootstrap.ts`.
The standalone product does not build the workbench Wasm module, while the shared
TypeScript check imports its generated, ignored `web/src/wasm/pero_ui` bindings.
Existing local Wasm output hid the missing source-only declaration.

Keep the shared checks and standalone build scope. Declare the small workbench
Wasm import surface for source-only checks, and verify checking both with and
without generated bindings. Preserve the failed tag and publish a new tag
containing the correction. All four native packages must still pass verification
before publication.

Added a typed ambient import declaration for the six functions consumed by the
workbench bootstrap. The normal generated module remains available for workbench
builds. In the clean verification checkout, `npm run check` passed both with
the generated directory removed from its import path and after restoring it.
No workflow gate was removed or made conditional. Native release acceptance
will be recorded against the replacement version tag.
