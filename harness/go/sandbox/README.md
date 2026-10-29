# Strict headless native execution (opt-in)

This package copies Point `go/harness/sandbox/strict.go` at
`edff76d7dd39b2900cff707af1896fe8ff9fd2dc` and adapts only the minimal policy/provider
closure. It does not copy Point UI, Projects, deployment defaults or unrestricted
runners. Existing unsandboxed behavior remains explicit and unchanged when no
sandbox option is supplied; requesting strict mode never falls back to it.

## Contract

Trusted startup config selects `read-only` or `workspace-write`, a canonical,
non-root, existing workspace, and 1–32 explicit absolute runtime mounts. Runtime
mounts must be trusted system tools/dependencies, not user data. No model, tool
arguments, repository config, session event, environment variable or approval may
widen this config. Callers must protect the workspace's ancestor directories and
runtime trees from concurrent untrusted replacement. This is not a resource quota
or VM boundary. Do not mount secret-bearing trees. Unix sockets/FIFOs/devices in
runtime trees are rejected; workspace inspection also rejects sockets/FIFOs/devices and regular files with
multiple hard links before grant and execution. No concurrent external workspace
mutation is permitted during admission/launch. Do not include secrets the child
is not meant to access.

Linux uses fixed `/usr/bin/bwrap`, user/PID/network/IPC/UTS namespaces, capability
drop, an otherwise empty read-only root, explicit read-only runtime binds, a
workspace bind (read-only or writable), private scratch, and cleared environment.
Darwin uses fixed `/usr/bin/sandbox-exec` and Point's deny-default Seatbelt profile.
Network is denied in both profiles. Unsupported OS, missing runner or failed
fixed `/usr/bin/true` probe fail closed before any candidate command is launched.
No tools are installed and no host sandbox settings are changed automatically.
Runtime mounts must supply that probe and the requested shell's loader/libraries.

Native read-only composition omits `write_file`/`edit_file`. File operations remain
bounded descriptor-rooted operations, not child-process OS confinement. Shell uses
existing 120s maximum, bounded output and cancellation ownership. Every shell still
requires approval; sandbox confinement is not approval. The native approval
callback checks CWD, validates shell input and probes fixed trusted code only.
Its `enforcement: required` describes a fail-closed launch requirement, NOT a claim
that the future child already exists. Run repeats validation before launch.
Unconfined shell and write/edit do not supply native auto-approval descriptors.

## Verification limits

Profile fixtures verify Linux and Darwin policy generation. Actual Linux probe
in this execution environment finds bwrap but exits 1: strict runtime is unavailable
here and candidate commands are not launched. The runtime test records a skip,
not successful confinement. On a supported deployment it checks an allowed write,
outside read/write denial, and inability to connect to a listening host loopback
port. Darwin runtime enforcement has NOT been tested here. A production deployment
must pass these native checks with its exact trusted mounts before activation.

## Explicit Windows partial backend

`Config{Backend: BackendWindowsACL, Mode: ReadOnly|WorkspaceWrite}` is a separate
Windows-only opt-in. Empty/default or `BackendStrict` never chooses it. The CLI
presets are `windows-acl-read-only` and `windows-acl-workspace-write`; they require
an explicit absolute PowerShell 7 executable, `--enable-shell`, a workspace and
`--approval=ask`. Runtime mounts are rejected, not treated as read confinement.
The explicit partial backend adds `shell` alongside independently handle-rooted
read/list/search/create-new file tools. Read-only mode omits file creation.
Existing-file replacement/edit remain unavailable; see [file boundaries](../fstools/README.md).

The result is **`enforcement: partial`, `readIsolated: false`,
`networkIsolated: false`**. Reads, network, console/IPC and ambient permissions are
not a strict boundary. Even read-only is a write-restricted token, not a promise
of zero writes everywhere: Everyone/logon rights on already-Low objects can still
apply. Do not expose host-readable secrets. This does not provide VM, resource or
full filesystem isolation, and partial shell calls never get strict native
approval descriptors or automatic approval.

The Go backend reuses Point's capability SIDs, restricted token and Job Object
lifecycle, and aligns the pinned official DSH protections:

- `DISABLE_MAX_PRIVILEGE | WRITE_RESTRICTED`, Low token integrity, and a default
  DACL that prefers the private temp capability (workspace next; Everyone for
  read-only). Read-only carries neither workspace nor temp capability.
- A workspace-write grant atomically adds the capability allow, an Everyone
  **container-only** `FILE_DELETE_CHILD` deny, and inheritable Low no-write-up
  label. Exact matching checks all three, repairing older capability-only grants.
  The directory deny intentionally prevents directory FullControl opens; regular
  files do not inherit that meaningless delete-child bit.
- Stable workspace grants and Low labels **persist after execution**, as in DSH.
  This is a real security-descriptor change to the selected workspace and its
  inheriting children; provision only an explicitly authorized owned workspace.
  It is not rolled back as though a saved DACL snapshot were safe to restore.
- Private temp is a fresh disjoint canonical directory with its own capability.
  Revocation is registered before grant application and merges out only that
  capability, clearing Low only when no other capability grant remains. The
  shared directory deny stays; the temporary tree is then removed.
- Spawn suspended, attach to a kill-on-close Job, then resume. Only NUL stdin and
  output handles reach the candidate, not token/Job/control handles. Parent EOF
  cancels the Job; normal completion also terminates surviving descendants and
  observes an empty Job before temp cleanup. A forced runner kill can leave temp
  security edits/residue; shell reports cleanup uncertainty rather than success.

The caller owns ancestor directories and must exclude concurrent external tree
mutation during validation/launch. Reparse points and hardlink aliases in the
workspace are rejected. Grant serialization retains Point's per-path **same-logon
named mutex**, not DSH's cross-logon lock-file semantics. This iteration does not
add shared cross-logon provisioning or additional writable roots. The probe runs
only a fixed read-only self-child and never modifies workspace ACLs before shell
approval; failed setup never launches candidate code unconfined.

### Windows verification

Ordinary tests do not mutate Windows security descriptors. The explicit
`PERO_REQUIRE_WINDOWS_ACL=1` gate runs only on the disposable Windows Actions
runner; all ACL/integrity changes are confined to test-owned temporary trees,
never the checkout. Missing backend support fails that gate, not a successful
skip. The matrix checks Low token/ACL state and weak-grant repair, read-only
downgrade, permitted workspace writes/deletes, denied outside writes and
parent-delete bypass, deliberately permitted outside reads/loopback networking,
sibling private-temp isolation, temp revocation, descendant cleanup, and the
actual PowerShell shell cancellation/approval composition, and the built ACP CLI
through a synthetic loopback provider and approve/reject flow. Additional
fixtures prove Medium Everyone-Modify writes are blocked by Low integrity,
foreign Low-labelled roots resist deletion, canonical junction/temp overlap and
hardlinks fail before grants, and forced runner death kills descendants while
explicitly leaving unreclaimed temp residue for host cleanup.

Linux unit/race checks and Windows cross-compilation do **not** establish native
Windows enforcement. The positive Windows CI result for the exact commit is
required before calling this backend runtime-verified. Other DSH services,
AppContainer/network restrictions and full upstream parity remain outside this
change. The separately documented Windows file subset does not strengthen the
shell's read/network isolation.
