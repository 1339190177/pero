# Product distributions

Status: implemented first slice, 2026-10-28.

## Model

An **App** owns domain UI, backend entry and its existing versioned `app.json`.
A **Product** owns delivery identity, version, included Apps, shell and runtime
policy. A **Distribution** is the assembled output for the build machine's
platform and architecture; it is not a new runtime service or state machine.

`packages/product/product.ts` is the immutable Product type and the single owner
of Product definition validation. Its pure parser performs no I/O. The build
and Electron composition roots read configuration and pass it through that type.
The builder checks that the selected entry exists in the selected App manifest.

- `workbench` has no fixed App entry. `products/pero.json` supplies the existing
  Pero packaging defaults; the established workbench command remains available.
- `app` must select a panel from its included Apps. Virtual Cell is the first
  product and has a separate macOS bundle ID, profile and launch entry.
- `python: bundled` requires a supplied relocatable Python directory containing
  its standard library, shared libraries and licenses. The builder copies it and
  verifies the copied interpreter. It does not download or resolve a runtime.
- `python: system` explicitly selects the machine interpreter for development
  distributions. A bundled product never falls back to system Python.

## Composition

The App shell is a full-window instance of the existing Web App surface. It
loads no GPUI Wasm navigation. Electron reuses `DesktopApps` for Host requests,
private credentials and `pero-app://` serving. App frames have no Electron
preload. Rust Host remains the one owner of installation and backend children.

Product preferences use the existing Host runtime-settings file. Language is
persisted there; theme follows the OS with session menu overrides. Domain data
lives in the existing Host `app-data` directory beneath the product profile.
The single-App shell exposes no installation/removal interface.

The macOS packager accepts the staged Distribution, retains the established
workbench packaging path and generates a product-named App/DMG. The App's fixed
internal executable name remains `Pero`; display name, bundle identity and
version come from Product. Standalone products do not register Pero OAuth links
or include Agent adapters. Output sizes are reported without applying the
existing Pero workbench's 100 MB budget to a Python-bearing product.

## Build and trial

```sh
npm run build:product -- --product virtual-cell --python /path/to/relocatable/python --package
npm run test:product
npm run test:product:e2e
```

The first command emits `target/products/virtual-cell` and
`target/products/package-com.openpero.virtualcell/Virtual Cell.app` plus a DMG.
This first builder targets the current machine; it does not implement cross
compilation. `--python` is explicit so local absolute paths never enter the
committed product definition.

## Limits

This slice does not implement App upgrades, data migrations, runtime downloads,
auto-updates, App sandboxing, a marketplace or a generic recipe system. The
existing one-time builtin seed means an existing profile retains its installed
App version; use a new trial profile for this first build. A release upgrade
policy needs a separate design before distributing updates to existing users.
Signing is ad-hoc, with no notarization or production release claim. Scientific
acceptance is independent from packaging and UI verification.

See [product verification](../fitness/products.md) for observed trial results.
