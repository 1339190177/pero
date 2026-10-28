---
kind: issue
status: active
date: 2026-10-29
---

# Native release first-run verification

The first multi-OS release is being verified in Actions run 40784807820.
Regular CI run 40784799949 failed on Linux before compiling the UI: Cargo refused
its `--locked` build because the registry-based lock still needed the pinned
local GPUI patch resolution. The release workflow already performs that resolution;
regular Desktop CI did not. This is a build preparation mismatch, not a passing
native test or a released-package result.

The bounded fix adds the same `scripts/cargo.mjs check -p pero-contracts` step
before regular Desktop builds. It retains locked compilation after preparation.
The new CI run and remaining release platforms must be observed before closeout.

The first native launch then found Linux WebGPU initialization unavailable on the
headless runner. Regular CI also exposed canonical Windows device paths being
passed to Node/Git, plus a macOS Python test startup timeout. The shell now uses
`dunce::canonicalize` at path entry so ordinary Windows paths cross process and
JSON boundaries. The HTTP lifecycle fixture binds TCP directly and avoids
HTTPServer's reverse DNS lookup; it still waits for the backend readiness line.
The Linux launch check will explicitly select the CI software Vulkan adapter and
retain browser diagnostics. None of these failures is accepted as a passing gate.

Windows packaging also selected Git Bash GNU tar from PATH, which interpreted
`D:` as a remote archive host and cannot emit the intended ZIP. The Windows
packager now explicitly invokes System32/tar.exe (Windows bsdtar) with ZIP mode.
The earlier candidates remain unpublished; tags are not rewritten.

A subsequent Windows regular CI run restored an existing fontTools venv without
working pip. The font tool health check rejected it, but the setup path skipped
venv creation solely because python.exe existed. Invalid tool environments now
recreate their venv with `--clear` before installing pinned fontTools. A healthy
cached environment continues to be reused.

The initial tag-triggered retries could not restore each other's Cargo caches:
GitHub isolates tag cache scopes. Future tag events now dispatch the native build
on the default branch, while checkout and package metadata remain pinned to the
tag. This avoids rebuilding the same dependencies for every new release tag.

The explicit Vulkan attempt still had no usable WebGPU adapter and could not
create WebGL2. Browser logs and the failure screenshot confirmed the renderer
never became ready. A separate graphics probe (Actions 40789552506) then passed
on both Ubuntu 24.04 and Windows 2025 using ANGLE SwiftShader WebGL2, including
RGBA pixel readback. Packaged non-macOS smoke now uses those same three flags.
GPUI already owns the WebGPU-to-WebGL2 fallback and creates a fresh canvas for
it; no renderer implementation or platform test gate is bypassed.
