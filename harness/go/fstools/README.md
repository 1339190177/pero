# Workspace text files

Explicit launch-time workspace authority on Linux/macOS/Windows; unsupported
platforms fail closed at construction. This is not an OS sandbox. The workspace is
opened as an `os.Root` capability. Paths must be canonical relative slash paths;
absolute paths, traversal, selected symlinks and special files are rejected.
The reused Unix scoped-text algorithm pins directory descriptors, checks
preimages, stages replacement, synchronizes and verifies readback. Every scope must match
the originally selected workspace identity, not a replacement pathname.

Reads are bounded to UTF-8/NUL-free regular files ≤1 MiB. Directory listing is
at most 256 entries. Literal search visits at most 512 entries and 8 MiB text,
returning at most 64 matches. Context cancellation is checked during traversal.
No recursive directory creation, deletion, image read or unrestricted glob exists.

Creation uses exclusive open and does not silently overwrite a raced target.
An error after creation/commit can leave an effect: receipts remain `unknown`
until reconciliation, not a promise of rollback. External concurrent writers are
not locked out; inspect actual state before retrying. Replacement uses a new
inode, so editing a hard-linked file does not modify another link's contents.
A hard link already present in an approved workspace is still readable: workspace
capabilities do not infer file provenance or provide user/OS access isolation.

`SOURCE.json` records precise upstream algorithms. No blanket source license is
invented. This package adds the narrow workspace adapter rather than restoring
Point sandbox/Host/product or FS-observation services.

## Windows native subset

Windows supports bounded read/list/search and **create-new only**. Child opens
use `NtCreateFile` relative to pinned directory handles with `OBJ_DONT_REPARSE`;
all reparse tags, streams, device names and noncanonical DOS aliases are rejected.
The explicitly selected launch root may resolve through a junction; its resolved
handle identity is pinned. Existing hardlinks remain readable as documented above.

Creation writes a randomly named sibling, flushes its content, and publishes with
handle-relative `FileRenameInformation` and `ReplaceIfExists=FALSE`. Competing
regular files, hardlinks and reparse points are never overwritten. New files
inherit the selected parent's ACL; this backend does not change ACLs or labels.
Prepublication cancellation deletes only the owned stage handle. A publication
error with uncertain effect returns `unknown` and may retain a staging file for
reconciliation. Observed readback is not a Windows directory-fsync or power-loss
transaction guarantee.

Existing-file `Write`, `Edit` and the public writable `OpenScopedTextFiles` API
remain unavailable on Windows. DSH's pathname-based `ReplaceFileW` preserves
DACLs, streams and other metadata but reopens names; a simple rooted rename would
silently discard that metadata. Neither weaker implementation is used. Skills
still depend on the unsupported writable-scoped constructor; explicit root
AGENTS.md instruction loading can reuse the workspace's read capability.
