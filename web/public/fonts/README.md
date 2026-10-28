# Build-time fonts

Font binaries and `.fonts-stamp.json` are generated assets, ignored by Git.
`npm run build:wasm` prepares them before Rust embeds them. To prepare only fonts:

```sh
node scripts/build/fonts.mjs
```

The build first tries complete, validated output fonts in `PERO_FONT_DIR`, then
`PERO_GPUI_KIT_PATH/crates/story-web/fonts`. It checks font tables, family names
and 400/700 weights; the pinned Inter subset retains its original variable font
tables. A complete local set avoids Python and downloads.

```sh
PERO_FONT_DIR=/absolute/path/to/fonts node scripts/build/fonts.mjs
```

The provider can contain all seven output names listed in the manifest, or
checksum-matching source names/aliases for generation. `PERO_GPUI_KIT_PATH` selects
the same checkout used by the Cargo wrapper.
The generated cache is checked by input fingerprint and per-file SHA-256.

For missing outputs, source lookup uses `PERO_FONT_DIR`, the GPUI Kit checkout,
then `target/font-cache/sources`. Every source must match the SHA-256 in
[scripts/build/fonts.manifest.json](../../../scripts/build/fonts.manifest.json).
Only after these local sources are exhausted does the build download pinned HTTPS
sources. Downloads and generated outputs are written atomically. No font data or
cache stamp should be committed. Keep this README and `OFL.txt` in Git.

The three Latin fonts are the GPUI Kit subsets at snapshot
`ec89cfc36404667b7947aeb3d40b1c2859d729dd`: Inter, IBM Plex Sans and JetBrains Mono.
Noto Sans SC comes from Google Fonts snapshot
`9b59ea9f333c5b8a99fa44e2a698496d6cde5b8e`. The manifest records exact source URLs.

The Python build pins fontTools 4.60.1. It uses an existing matching installation
(including an explicitly supplied `PYTHONPATH`) or creates a cached environment
under `target/tools/`. `PERO_PYTHON` can select the bootstrap Python interpreter.
An offline build needs either complete output fonts, or cached sources plus the
cached fontTools environment.

Noto produces full static 400/700 instances named `Noto Sans SC Full`. The
`Workbench-regular.ttf` and `Workbench-bold.ttf` subsets include printable ASCII
and characters from both locale catalogs, with family `Noto Sans CJK SC` to match
the Workbench theme. The full fonts remain available for other Chinese content.
Source copyright and SIL Open Font License metadata are retained when deriving
the fonts; the license text is in `OFL.txt`.
