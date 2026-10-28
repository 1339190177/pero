import { existsSync } from 'node:fs';
import { resolve } from 'node:path';
import { root } from './paths.mjs';

export const kit = resolve(process.env.PERO_GPUI_KIT_PATH || resolve(root, '../../test/gpui-kit'));

export function cargoArgs(args) {
  if (!existsSync(resolve(kit, 'crates/kit/Cargo.toml'))) {
    throw new Error('Set PERO_GPUI_KIT_PATH to your gpui-kit checkout (see desktop/README.md).');
  }
  const crates = { 'gpui-kit': 'kit', 'gpui-base': 'base', 'gpui-component': 'component',
    'gpui-component-macros': 'component-macros', 'gpui-kit-assets': 'assets' };
  const toolchain = args[0]?.startsWith('+') ? [args.shift()] : [];
  return [...toolchain, ...Object.entries(crates).flatMap(([name, path]) => [
    '--config', `patch.crates-io.${name}.path=${JSON.stringify(resolve(kit, 'crates', path))}`,
  ]), ...args];
}
