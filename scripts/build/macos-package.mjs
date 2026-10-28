// Portable macOS development package. Agent CLIs are supplied by the user.
import { cpSync, mkdirSync, readFileSync, writeFileSync, rmSync, readdirSync, statSync, symlinkSync, renameSync } from 'node:fs';
import { resolve, join } from 'node:path';
import { execFileSync } from 'node:child_process';
import { build } from 'esbuild';
import { Product } from '../../packages/product/product.ts';
const root = resolve(import.meta.dirname, '../..');
if (process.platform !== 'darwin') throw new Error('macOS packaging requires macOS.');
const staged = process.env.PERO_PRODUCT_PAYLOAD;
const product = Product.parse(JSON.parse(readFileSync(staged ? join(staged, 'product.json') : resolve(root, 'products/pero.json'), 'utf8')));
const destination = staged ? resolve(staged, '../package-' + product.id) : resolve(root, 'target/package');
const output = join(destination, `${product.name}.app`);
const source = resolve(root, 'node_modules/electron/dist/Electron.app');
mkdirSync(destination, { recursive: true });
rmSync(output, { recursive: true, force: true });
cpSync(source, output, { recursive: true, verbatimSymlinks: true });
renameSync(join(output, 'Contents/MacOS/Electron'), join(output, 'Contents/MacOS/Pero'));
const resources = join(output, 'Contents/Resources');
rmSync(join(resources, 'default_app.asar'), { force: true });
const frameworkResources = join(output, 'Contents/Frameworks/Electron Framework.framework/Versions/A/Resources');
for (const directory of [resources, frameworkResources]) {
  for (const name of readdirSync(directory)) {
    if (name.endsWith('.lproj') && !['en.lproj', 'en_US.lproj', 'zh_CN.lproj'].includes(name)) {
      rmSync(join(directory, name), { recursive: true });
    }
  }
}
const payload = join(resources, 'app');
if (staged) {
  cpSync(staged, payload, { recursive: true, verbatimSymlinks: true });
} else {
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
cpSync(resolve(root, 'target/release/pero-host'), join(payload, 'bin/pero-host'));
execFileSync('/usr/bin/strip', ['-x', join(payload, 'bin/pero-host')]);
writeFileSync(join(payload, 'package.json'), JSON.stringify({ name: 'openpero', version: product.version, main: 'desktop/dist/main.cjs' }));
cpSync(resolve(root, 'node_modules/electron/LICENSE'), join(payload, 'Electron.LICENSE'));
cpSync(resolve(root, 'node_modules/electron/dist/LICENSES.chromium.html'), join(payload, 'LICENSES.chromium.html'));
// Adapters are small JavaScript bundles; neither native Agent CLI is included.
const adapters = join(payload, 'adapters');
mkdirSync(adapters, { recursive: true });
for (const [name, entry, external] of [
  ['codex-acp', '@agentclientprotocol/codex-acp', []],
  ['claude-agent-acp', '@agentclientprotocol/claude-agent-acp', ['@anthropic-ai/claude-agent-sdk']],
]) {
  const packageRoot = resolve(root, 'node_modules', entry);
  await build({ entryPoints: [join(packageRoot, 'dist/index.js')], outfile: join(adapters, `${name}.mjs`),
    bundle: true, minify: true, platform: 'node', format: 'esm', external,
    banner: { js: "import { createRequire as packageRequire } from 'node:module'; const require = packageRequire(import.meta.url);" } });
  cpSync(join(packageRoot, 'LICENSE'), join(adapters, `${name}.LICENSE`));
}
const sdk = join(payload, 'node_modules/@anthropic-ai/claude-agent-sdk');
mkdirSync(resolve(sdk, '..'), { recursive: true });
cpSync(resolve(root, 'node_modules/@anthropic-ai/claude-agent-sdk'), sdk, { recursive: true,
  filter: path => !path.endsWith('.map') && !path.endsWith('.d.ts') });
// Use Electron's existing Node runtime; no separate Node distribution.
writeFileSync(join(payload, 'bin/node'), '#!/bin/sh\nbase="$(CDPATH= cd -- "$(dirname -- "$0")/../../.." && pwd)"\nexport ELECTRON_RUN_AS_NODE=1\nexec "$base/MacOS/Pero" "$@"\n', { mode: 0o755 });
mkdirSync(join(payload, 'node_modules/.bin'), { recursive: true });
for (const [name, key, executable] of [['codex-acp', 'CODEX_PATH', 'codex'], ['claude-agent-acp', 'CLAUDE_CODE_EXECUTABLE', 'claude']]) {
  writeFileSync(join(payload, 'bin', name), `#!/bin/sh\nbase="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"\nexport ${key}="\${${key}:-${executable}}"\nexec "$base/bin/node" "$base/adapters/${name}.mjs" "$@"\n`, { mode: 0o755 });
  symlinkSync(`../../bin/${name}`, join(payload, 'node_modules/.bin', name));
}
}
// Generate platform assets from the Product's SVG; keep generated binaries out of source.
if (product.icon) {
  const svg = resolve(staged || resolve(root, 'products'), product.icon);
  const iconset = join(destination, 'product.iconset');
  rmSync(iconset, { recursive: true, force: true });
  mkdirSync(iconset, { recursive: true });
  for (const size of [16, 32, 128, 256, 512]) for (const scale of [1, 2]) {
    execFileSync('rsvg-convert', ['--width', String(size * scale), '--height', String(size * scale),
      '--output', join(iconset, `icon_${size}x${size}${scale === 2 ? '@2x' : ''}.png`), svg]);
  }
  execFileSync('/usr/bin/iconutil', ['--convert', 'icns', '--output', join(resources, 'product.icns'), iconset]);
  rmSync(join(resources, 'electron.icns'), { force: true });
}
const plist = join(output, 'Contents/Info.plist');
let content = readFileSync(plist, 'utf8')
  .replace(/(<key>CFBundleExecutable<\/key>\s*)<string>Electron<\/string>/, '$1<string>Pero</string>')
  .replace('<string>com.github.Electron</string>', `<string>${product.id}</string>`)
  .replace(/(<key>CFBundle(?:DisplayName|Name)<\/key>\s*)<string>Electron<\/string>/g, `$1<string>${product.name}</string>`)
;
if (!staged) content = content.replace('</dict>\n</plist>', '<key>CFBundleURLTypes</key><array><dict><key>CFBundleURLName</key><string>Pero Desktop</string><key>CFBundleURLSchemes</key><array><string>pero</string></array></dict></array></dict>\n</plist>');
content = content.replace(/(<key>CFBundleShortVersionString<\/key>\s*<string>)[^<]+/, `$1${product.version}`);
if (product.icon) content = content.replace(/(<key>CFBundleIconFile<\/key>\s*<string>)[^<]+/, '$1product.icns');
writeFileSync(plist, content);
execFileSync('/usr/bin/plutil', ['-lint', plist], { stdio: 'inherit' });
execFileSync('/usr/bin/codesign', ['--force', '--deep', '--sign', '-', output], { stdio: 'pipe' });
execFileSync('/usr/bin/codesign', ['--verify', '--deep', '--strict', output], { stdio: 'pipe' });
const image = join(destination, `${product.name}-macos-${process.arch}.dmg`);
rmSync(image, { force: true });
execFileSync('/usr/bin/hdiutil', ['create', '-volname', product.name, '-srcfolder', output, '-format', 'ULMO', image], { stdio: 'inherit' });
function bytes(path) {
  const stat = statSync(path);
  return stat.isDirectory() ? readdirSync(path, { withFileTypes: true }).reduce((total, entry) => total + (entry.isSymbolicLink() ? 0 : bytes(join(path, entry.name))), 0) : stat.size;
}
const report = { app: output, appBytes: bytes(output), dmg: image, dmgBytes: statSync(image).size, limitBytes: staged ? null : 100_000_000, bundledAgentCLIs: false };
writeFileSync(join(destination, 'size.json'), JSON.stringify(report, null, 2) + '\n');
console.log(report);
if (!staged && report.dmgBytes >= report.limitBytes) throw new Error('DMG exceeds the 100 MB download budget. See target/package/size.json.');
