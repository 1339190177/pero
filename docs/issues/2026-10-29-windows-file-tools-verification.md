---
kind: verification_report
---

# Windows native file-tool subset

## Scope and status

PR #4 continues from `0f49e9e28bb8768ba331a7354ba9b44b0baf5cae`.
Implementation and local compilation/race checks are complete; **native Windows
acceptance is pending the exact published head's CI**. No Windows execution was
performed in the Linux editing environment. No merge or deployment is included.

The supported Windows surface is bounded `read_file`, `list_directory`,
`search_files`, and approved **create-new-only** `write_file`. `edit_file` is not
registered. Existing-file writes, including identical content and hardlinks, are
rejected without mutation. Public writable scoped selections remain Unix-only;
explicit root AGENTS.md loading reuses the workspace read capability. Skills,
Memory #9, product services and background shells are not expanded.

## Source comparison and intentional limit

The primary behavioral baseline is official DSH
`f3803c96974ad4a94195fb97e5c637e9c2a3afe3`, particularly
`packages/fs/fs-local/src/fsio.ts`, `win32.ts`, and `packages/fs/tool-fs/src/`.
Point `edff76d7dd39b2900cff707af1896fe8ff9fd2dc` still rejects Windows in its
scoped Go file implementation; its Windows `root.Open` helper is not a finished
native file port.

DSH copies an existing file's DACL to empty staging before writing, then uses
`ReplaceFileW` to merge destination metadata. Microsoft documents preserved
creation time, short name, object ID, DACL, resource attributes,
encryption/compression and named streams. The pathname API reopens names, while
a simple handle-relative rename would discard metadata. A safe equivalent is
**not** claimed or substituted. The bounded increment therefore creates new files
only; full metadata-preserving replacement/edit remains an explicit gap.

References:
- [DSH Windows implementation](https://github.com/deepseek-ai/deepseek-harness/blob/f3803c96974ad4a94195fb97e5c637e9c2a3afe3/packages/fs/fs-local/src/win32.ts)
- [Microsoft ReplaceFileW metadata contract](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-replacefilew)
- [Microsoft handle-relative rename](https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/ntifs/ns-ntifs-_file_rename_information)

## Enforced boundary

- Retain the existing `os.Root` and parent identity checks. Windows content opens
  use `NtCreateFile`, a pinned `RootDirectory`, `OBJ_DONT_REPARSE`, and an opened
  handle reparse-attribute check; no fallback to pathname I/O after rejection.
- Resolve the explicit launch root through `GetFinalPathNameByHandle`; retain
  eager handle identity, not a delayed pathname-based Windows file ID lookup.
- Reject selected reparse tags, traversal, ADS, device names and trailing-dot/space
  aliases. Preserve the existing documented ability to read an already-present
  hardlink; never modify an outside alias through replacement.
- Stage creation in the selected parent with an exclusive owned handle, flush
  content, then publish by handle-relative `FileRenameInformation` with
  `ReplaceIfExists=FALSE`. Target visibility is atomic; no Unix directory-fsync
  or power-loss transaction guarantee is claimed on Windows.
- Before publication, cancellation removes the stage by handle. A known
  no-replace collision preserves the competing target. Ambiguous publication or
  postcommit readback/finalization returns `unknown`, not an automatic retry or
  a promise of rollback; uncertain publication may leave a stage for reconciliation.
- New files inherit the destination directory ACL. No new ACL/label mutation is
  implemented or tested in this increment. The separately authorized existing
  eight partial-shell security contracts remain unchanged in security scope.
- Existing approvals, read-only mutation omission, strict Windows refusal and
  partial-shell read/network limits remain intact. File tools require no shell.

## Verification

Local Linux Go 1.26.4 checks passed: all 25 package race suites, vet and build.
Windows amd64 vet/build and native-test cross-compilation passed; fstools tests
also cross-compiled for Windows arm64. Local executable build checks use
`-buildvcs=false`: Go discovers the protected ancestor `.git` instead of the
worktree's `.git` file in this cloud workspace; this is not a production flag.
The locked standalone Gazelle no-diff check passed. The standalone Bazel
race build/test completed successfully: all 25 test targets passed. Current
destination-hash checks passed; the largest Go file remains 956 lines.
Independent read-only source/security review found no blocking issue.

The Windows gate explicitly requires 22 named native outcomes, with no missing
or skipped tests: the previous eight shell/ACL contracts, seven file-read/path/
junction/identity/limit contracts, four exclusive-creation/cancellation/collision
contracts, read-only file-tool omission, a real built CLI file/approval contract,
and the native root-instruction combined CLI case. All model interactions use
scripted loopback fixtures; no paid or external model calls occur.

The real built CLI proves read/list/search, approval rejection before creation,
approved create/readback, rejection of an existing hardlinked target without
changing the outside alias, create-only schema wording, and absent edit/shell
capabilities when shell is not enabled. Native creation tests observe target
absence before publication and actual no-replace collision after staging.

Linux/static/cross-compilation results do not substitute for that native gate.
The parent task owns publication to the existing draft PR and exact-head CI
monitoring; this record intentionally does not claim a native pass yet.

## Integration with current master

The first published file-subset head was
`2d1ee3a9165ba6d451e910cb15d55cd3f73ccada`. GitHub reported merge conflicts and
therefore had not started its PR CI. A real two-parent merge brings
`c611dc6802a2548e23a6f0472ce372778ade45ec` **into the feature branch**, preserving
master's local-worker/context/composer work and the `d84e100` lockfile repairs;
this is not merging the PR into master.

Six conflicts were resolved by preserving both sides: runtime `url` and master's
tree-sitter dependencies in Cargo.lock; native approvals plus master's agent
allowlist/child-owned lease; both remote test groups; and both ACP fixture/test
groups with a unique retained workspace per launch, including MCP launches.
The migrated shared WorkspaceLease retains the old Worker lock's Unix
`O_NOFOLLOW` refusal, with a real temporary symlink-negative regression. There
is still one lease owner rather than a duplicate supervisor lock.

Locked offline Rust tests and build passed for contracts/runtime/Worker/Host,
including all 22 distinct ACP lifecycle tests, 14 Worker unit tests and 10 remote
contracts. The shared lease redirect test passed. Harness Go race/vet/build and
Windows test cross-compilation passed again after integration; the harness and
22-outcome Windows gate are unchanged. Two independent read-only resolution
reviews found no blockers. Exact-head native Windows CI remains pending.
