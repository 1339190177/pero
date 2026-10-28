# Workspace text files

Explicit launch-time workspace authority on Linux/macOS; unsupported platforms
fail closed at construction. This is not an OS sandbox. The workspace is
opened as an `os.Root` capability. Paths must be canonical relative slash paths;
absolute paths, traversal, selected symlinks and special files are rejected.
The reused scoped-text algorithm pins directory descriptors, checks preimages,
stages replacement, synchronizes and verifies readback. Every scope must match
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
