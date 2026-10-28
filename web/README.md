# Shared Workbench

`src/bootstrap.ts` loads `pero-ui` WASM and adapts its commands/events to the
desktop bridge. The visible workbench is rendered in Rust, not duplicated HTML.
`src/theme.generated.json` is generated from `DESIGN.md`; run
`npm run gen:theme` after changing colors. The UI follows the
reference Workbench's navigation, task list, detail tabs and restrained borders.

Fonts in `public/fonts` include the original Workbench's regular/bold Chinese
fonts and complete Noto Sans SC fallbacks, with their OFL license. The Web
backend uses the same explicitly registered family for the composer and text.
Generated WASM is ignored; regenerate with `npm run build:wasm`.
