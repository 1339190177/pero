// Shared workbench payload for native Electron distributions.
import { cpSync, mkdirSync, readFileSync, writeFileSync, rmSync, readdirSync } from 'node:fs';
import { resolve, join } from 'node:path';
import { execFileSync } from 'node:child_process';
import { stageAgentAdapters } from './agent-adapters.mjs';
const root = resolve(import.meta.dirname, '../..');

export async function stageWorkbench(payload, product) {
  mkdirSync(join(payload, 'desktop/dist'), { recursive: true });
  mkdirSync(join(payload, 'bin'), { recursive: true });
  cpSync(resolve(root, 'desktop/dist'), join(payload, 'desktop/dist'), { recursive: true });
  cpSync(resolve(root, 'web/dist'), join(payload, 'web/dist'), { recursive: true });
  // Builtin Apps include runnable assets and stdlib Python, never development dependencies.
  for (const appName of product.apps) for (const entry of ['app.json', 'dist', 'backend', 'examples', 'evidence', 'THIRD_PARTY_NOTICES.md', 'README.md', 'licenses']) {
    const source = resolve(root, 'apps', appName, entry);
    cpSync(source, join(payload, 'apps', appName, entry), { recursive: true, filter: path => !path.includes('__pycache__') });
  }
  // GPUI embeds these fonts in Wasm; Vite's public copy is unused duplication.
  const fonts = join(payload, 'web/dist/fonts');
  for (const name of readdirSync(fonts)) {
    if (/\.(ttf|otf|woff2?)$/.test(name) || name === '.fonts-stamp.json') rmSync(join(fonts, name));
  }
  const host = process.platform === 'win32' ? 'pero-host.exe' : 'pero-host';
  cpSync(resolve(root, 'target/release', host), join(payload, 'bin', host));
  const worker = process.platform === 'win32' ? 'pero-worker.exe' : 'pero-worker';
  cpSync(resolve(root, 'target/release', worker), join(payload, 'bin', worker));
  if (process.platform === 'darwin') {
    for (const binary of [host, worker]) execFileSync('/usr/bin/strip', ['-x', join(payload, 'bin', binary)]);
  }
  writeFileSync(join(payload, 'package.json'), JSON.stringify({ name: 'openpero', version: product.version, main: 'desktop/dist/main.cjs' }));
  cpSync(resolve(root, 'node_modules/electron/LICENSE'), join(payload, 'Electron.LICENSE'));
  cpSync(resolve(root, 'node_modules/electron/dist/LICENSES.chromium.html'), join(payload, 'LICENSES.chromium.html'));
  await stageAgentAdapters(payload);
}
