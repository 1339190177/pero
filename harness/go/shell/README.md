# Foreground shell executor

The reusable foreground execution, tail collector and Unix process-group
primitives are adapted from `phodal/point` commit
`69a1f8692725078c4ce7feaf497f5b80799ca44d`, `go/harness/shell`.
See `SOURCE.json` for exact source blobs and extraction changes.

`NewLocalExecutor` requires an explicit absolute Bash executable and workspace
directory. `Resolve` validates the launch directory remains within that root,
and checks the original workspace object identity before resolution and process
start. Replacing that root between operations fails closed; pathname races in
the final check/start interval are not eliminated. **This is not a filesystem
or network sandbox**: an approved Bash command
can access other paths and network resources as its OS user. Callers must obtain
approval before execution and use external isolation for untrusted commands.

Default timeout is 30 seconds, hard maximum 120 seconds. Commands are limited to
16 KiB. Retained output is bounded to at most 64 KiB independently for stdout and
stderr; excess output is discarded while the pipes continue draining. The source
tail algorithm retains the last bytes and reports truncation. No spill files,
background job ownership, persistent shell, model environment overrides, product
sandbox service or managed runtime is included.

Only explicit PATH/LANG/LC_ALL/TZ values may be supplied in BaseEnv; no process
environment is inherited. Fixed noninteractive presentation values are added.
Bash startup files are disabled. Cancellation/timeout terminate then kill the
owned process group even if the leader exits first. Pipe drain time is bounded.
Ordinary descendants are cleaned up on completion; deliberate process-group or
session escape is not prevented and requires OS-level isolation. Linux and
macOS are supported; other platforms fail closed at construction.

Source reuse is owner-authorized. No blanket license for the source is inferred
or assigned here; none of the selected source files carries a third-party
license notice. Repository licensing remains an owner decision.
