---
kind: verification_report
date: 2026-10-29
---

# Native release products

A version-tag push dispatches this workflow on the default branch, and the
build still checks out the exact supplied tag. This keeps release cache storage
in a reusable branch scope: GitHub does not allow one tag to restore another
tag's caches. Manual releases use `workflow_dispatch` on the default branch.
See [GitHub cache scope](https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching).

The release matrix has four independent native jobs:

| Product | Runner | Archive |
| --- | --- | --- |
| Pero | macos-26 / arm64 | Pero-macos-arm64.dmg |
| Pero | windows-2025 / x64 | Pero-windows-x64.zip |
| Pero | ubuntu-24.04 / x64 | Pero-linux-x64.tar.gz |
| Virtual Cell | macos-26 / arm64 | Virtual-Cell-macos-arm64.dmg |

Each job verifies source-tag identity and embedded product/platform metadata,
then launches its actual packaged executable from outside the checkout. macOS
also verifies architecture and deep strict ad-hoc signing. The workbench checks
Host readiness and the default `https://app.openpero.com` connection. Virtual
Cell runs its existing full product acceptance, including real bundled Python
computation, restart, language/theme and missing-runtime recovery.

One dependent publication job downloads all four successful artifacts, checks
the combined SHA256SUMS, uploads to a draft prerelease, and publishes only after
all uploads succeed. It refuses to replace an already published release.
Use a new version tag containing the packaging changes; rebuilding an older tag
does not bring newer packaging scripts into that tag.

The offline Product builder receives the runtime explicitly. The release-only
download script pins CPython 3.12.16 from python-build-standalone release 20261026,
including the upstream asset's SHA-256 in `scripts/release/python.json` and the
build metadata. Source: [upstream release](https://github.com/astral-sh/python-build-standalone/releases/tag/20261026).

The local observations below preceded the public release recorded in the final
section. Publishing the desktop assets did not update the hosted service.

## Observed locally, macOS arm64

- `npm run check`, `npm run test:product` and all Rust suites in `npm test` passed.
- `bazel test --config=ci //:checks`: all seven frozen checks passed. The npm
  repository input hash was regenerated for the new packaging command.
- actionlint and Bash syntax checks passed for the release workflow.
- The exact pinned Python archive downloaded and passed SHA-256 verification.
  `build:product -- --product virtual-cell --python <downloaded runtime> --package`
  produced a 127,524,305-byte DMG; the full `test:product:e2e` passed against it.
- `npm run package:desktop` built the actual Pero App and DMG. The packaged App
  launched outside the checkout with a clean profile, started its Host, registered
  its adapters and reported the default online Task Center without credentials.
- Both packaged ACP adapters returned their versions using Electron's Node
  runtime directly, without Unix wrappers or a separate Node installation.
- Contributor acceptance passed against the packaged App with actual local Git
  cloning, branch creation, ACP file edits, diff review, commit and push to an
  isolated bare repository. GitHub OAuth/PR and the provider remained fixtures.
  The scenario started without a checkout or gh login, exercised missing gh,
  unauthenticated gh, the official help URL, and rechecking after recovery.
- Screenshots of pre-clone guidance, the login warning before review, installation
  recovery and Chinese dark/narrow layout were visually inspected. Evidence is
  under `test-results/contribution/`, `test-results/product/` and
  `test-results/release/local-smoke.json`.

Those local trials did not establish Windows/Linux native acceptance. The
subsequent native CI and public-download observations are recorded below. macOS
signing is ad-hoc with no notarization; computation checks are not scientific acceptance.


## Public release and downloaded-package acceptance

Published prerelease: [v0.1.0-dev.20261029.3](https://github.com/phodal/pero/releases/tag/v0.1.0-dev.20261029.3).
Source: `2571af3692d830d39dd9dab9bbf383785062519a`.
[Release run 40790527292](https://github.com/phodal/pero/actions/runs/40790527292)
passed all four native build/launch jobs and publication. This manual run used
workflow snapshot ref `v0.1.0-dev.20261029.2` to reuse its completed cache scope,
but checked out and verified the exact `.3` source tag. Every downloaded build
manifest reports the `.3` tag, source revision and this run URL.

| Public archive | Download bytes |
| --- | ---: |
| Pero-linux-x64.tar.gz | 134,640,224 |
| Pero-macos-arm64.dmg | 98,697,053 |
| Pero-windows-x64.zip | 168,444,898 |
| Virtual-Cell-macos-arm64.dmg | 126,702,625 |

- Downloaded all 13 public assets. Combined SHA256SUMS verified all four archives
  and all four build manifests; manifest archive hashes also matched the bytes.
- Confirmed Windows PE and Linux ELF Host binaries are x86-64. Their actual
  packaged desktop launches passed on their native CI runners. Linux CI used
  the separately verified software WebGL2 path; hardware GPU coverage is not claimed.
- Mounted both public macOS DMGs read-only, copied their Apps out, verified deep
  strict ad-hoc signatures and arm64 executables, and ran the installed copies.
- The downloaded Virtual Cell passed all six product check groups: bundled
  Python computation, simulation, language/theme/narrow layout, restart with
  persisted input, and missing-Python retry recovery. No system Python fallback.
- The downloaded Pero passed the full contributor acceptance with an account
  without project grants or a pre-existing checkout, actual local clone/branch,
  ACP file edits, review, commit/push to a bare fixture, and PR registration recovery.
  Missing/unauthenticated gh and the official help link were exercised. The
  provider and GitHub OAuth/PR remained fixtures; no live PR or model request was sent.
- All nine regular cross-platform CI jobs passed in
  [run 40788990827](https://github.com/phodal/pero/actions/runs/40788990827).
  The native/runtime/desktop source in that CI revision matches the released tag;
  the release tag adds the separately tested graphics and workflow documentation.

GitHub changed space-containing Virtual Cell asset filenames to dots on upload.
Public-download verification caught the resulting checksum-name mismatch. The
published filenames were normalized to `Virtual-Cell`, and build/checksum text
metadata was repaired and downloaded again. The DMG was never rebuilt or
replaced: its original CI SHA-256 remains
`c0caf9f7aad096fc7b88bb22b6cf85a2017efe843f83a192a2eb575b2432f0ee`.
The build manifest retains `ciArchive` for its original CI filename.

The preventive Product.artifactStem change, shared by both packagers and release
metadata, is proposed in [PR #13](https://github.com/phodal/pero/pull/13). Product
naming cases and type/generated checks passed locally. Repackaging the downloaded
CI payload also emitted `Virtual-Cell-macos-arm64.dmg` while preserving the
`Virtual Cell.app` display name. The release remains
usable while that CI/build fix waits for review. Future CI changes use PRs;
this verification PR is not merged automatically.

Local evidence: `target/release-downloads/v0.1.0-dev.20261029.3/download-verification.json`,
`test-results/product/result.json`, `test-results/contribution/result.json` and
corresponding screenshots. The native GPUI developer preview was also rebuilt
and connected locally; its [separate record](native-preview.md) keeps its checkout
dependency and missing cloud/Web App support distinct from the four public archives.
