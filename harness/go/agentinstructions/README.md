# Explicit workspace instructions

`Mount(ctx, h, Options{Workspace: absolutePath})` opts in to loading exactly
`Workspace/AGENTS.md` before model steps. Close the returned App after Harness
Dispose/Drain. The host must validate session CWD with `App.CheckCWD`; each step
also checks the same descriptor-backed workspace authority.

No home, parent, nested, `CLAUDE.md`, local override, script, network, or product
profile discovery is performed. A missing file is optional. Symlinks, replaced
roots, invalid text and files over the existing 1 MiB reader bound are errors.
Rendered context is bounded to 64 KiB by default (configurable 1–64 KiB), with
UTF-8-safe truncation and an omission marker.

The file is untrusted user-role task guidance, not permission. Existing approvals
and filesystem policy remain authoritative. Changed or removed content produces
a replacement baseline; unchanged content is deduplicated from durable session
messages, including after JSONL reload. No extra database or mutable cache is
used. This is deliberately narrower than Point's dynamic instruction loader;
see SOURCE.json for reused renderer provenance and adaptations.
