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
