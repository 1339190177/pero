---
kind: verification_report
---

# Windows ACL sandbox native verification

The initial native Windows gate for PR #4 ran on a disposable Windows Actions
runner against `fef081d4333dcd3bce92d394745adf761611b951`:
[run 40820879808](https://github.com/phodal/pero/actions/runs/40820879808).
All ACL and integrity-label changes target test-created temporary directories,
not the checkout or a user workspace. The required positive gate does not accept
skipped or missing native tests.

Six of eight required tests passed: restricted runtime, Job lifecycle, ambient
Everyone-Modify / cross-Low-root boundaries, grant repair/revocation, shell-only
approval composition, and the actual built CLI's ACP/provider/write boundary.
This is not full upstream parity or a passing acceptance result.

Two failures prevented acceptance:

- The shell cancellation fixture compared its short `RUNNER~1` temporary-root
  spelling against the runner's canonical `runneradmin` path. Canonicalizing the
  fixture root fixes this identity mismatch; cancellation assertions remain.
- The junction-overlap case failed closed with an unqualified path-not-found
  error instead of proving the intended overlap rejection. The fixture now uses
  a canonical parent and verifies the junction's reparse attribute, target and
  file identity before launch. The runner receives the junction path, not its
  resolved target; the exact overlap-error and no-grant/no-launch assertions are
  unchanged. Native inspection/private-temp errors now identify their stage and
  path. The original cause needs the corrected native rerun; it is not presumed
  resolved by the fixture change.

The correction requires a new exact-commit Windows gate. Cross-compilation and
Linux tests cannot establish Windows ACL, cancellation or junction behavior.
No strict-policy downgrade or permission-test scope expansion is involved.
