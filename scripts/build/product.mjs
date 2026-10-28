import { readFileSync, writeFileSync, mkdirSync, cpSync, rmSync, existsSync } from 'node:fs';
import { resolve, join, dirname } from 'node:path';
import { execFileSync } from 'node:child_process';
import { build } from 'esbuild';
import { Product } from '../../packages/product/product.ts';
const root = resolve(import.meta.dirname, '../..');
const args = process.argv.slice(2);
const name = args[args.indexOf('--product') + 1];
if (!args.includes('--product') || !/^[a-z0-9-]+$/.test(name)) throw new Error('Use --product virtual-cell [--package] [--python /path/to/standalone/python]');
const config = resolve(root, 'products', name, 'product.json');
const product = Product.parse(JSON.parse(readFileSync(config, 'utf8')));
const icon = product.icon ? resolve(dirname(config), product.icon) : undefined;
if (icon && !existsSync(icon)) throw new Error(`Product icon missing: ${icon}`);
if (product.shell !== 'app') throw new Error('Use the existing package:macos command for the workbench');
const output = resolve(root, 'target/products', name);
// Validate the selected entry against its actual App before starting expensive builds.
for (const app of product.apps) {
  const manifest = JSON.parse(readFileSync(resolve(root, 'apps', app, 'app.json'), 'utf8'));
  if (manifest.name !== app || (app === product.entry.app && !manifest.contributes.panels.some(panel => panel.id === product.entry.panel))) throw new Error(`Product entry does not exist in ${app}`);
  execFileSync(process.execPath, [resolve(root, 'scripts/build/apps.mjs'), '--app', app], { stdio: 'inherit' });
}
execFileSync(process.execPath, [resolve(root, 'scripts/cargo.mjs'), 'build', '--release', '--locked', '-p', 'pero-host'], { stdio: 'inherit' });
rmSync(output, { recursive: true, force: true });
mkdirSync(join(output, 'desktop/dist'), { recursive: true });
mkdirSync(join(output, 'bin'), { recursive: true });
await build({ entryPoints: { product: resolve(root, 'desktop/src/main/product.ts'), 'product-preload': resolve(root, 'desktop/src/product/preload.ts') }, outdir: join(output, 'desktop/dist'), outExtension: { '.js': '.cjs' }, bundle: true, platform: 'node', format: 'cjs', external: ['electron'] });
const script = await build({ entryPoints: [resolve(root, 'desktop/src/product/shell.ts')], bundle: true, platform: 'browser', format: 'iife', write: false, minify: true });
const style = readFileSync(resolve(root, 'desktop/src/product/shell.css'), 'utf8');
writeFileSync(join(output, 'desktop/dist/product.html'), `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>${style}</style></head><body><script>${script.outputFiles[0].text}</script></body></html>`);
if (icon) {
  mkdirSync(dirname(join(output, product.icon)), { recursive: true });
  cpSync(icon, join(output, product.icon));
}
writeFileSync(join(output, 'product.json'), JSON.stringify(product, null, 2));
writeFileSync(join(output, 'package.json'), JSON.stringify({ name, version: product.version, main: 'desktop/dist/product.cjs' }));
cpSync(resolve(root, 'target/release/pero-host'), join(output, 'bin/pero-host'));
for (const app of product.apps) {
  for (const entry of ['app.json', 'dist', 'backend', 'examples', 'evidence', 'THIRD_PARTY_NOTICES.md', 'README.md', 'licenses']) {
    const source = resolve(root, 'apps', app, entry);
    if (existsSync(source)) cpSync(source, join(output, 'apps', app, entry), { recursive: true, filter: path => !path.includes('__pycache__') });
  }
}
if (product.python === 'bundled') {
  const pythonRoot = args.includes('--python') ? args[args.indexOf('--python') + 1] : process.env.PERO_PRODUCT_PYTHON_ROOT;
  if (!pythonRoot || !existsSync(resolve(pythonRoot, 'bin/python3'))) throw new Error('Bundled Python requires --python /path/to/relocatable/python (including its standard library and licenses)');
  cpSync(resolve(pythonRoot), join(output, 'runtime/python'), { recursive: true, verbatimSymlinks: true });
  // Verify the relocated interpreter, not the build machine interpreter.
  execFileSync(join(output, 'runtime/python/bin/python3'), ['-I', '-c', 'import json, http.server, ssl; print("Bundled Python ready")'], { stdio: 'inherit', env: { PATH: '/usr/bin:/bin' } });
}
console.log(`Product staged: ${output}`);
if (args.includes('--package')) execFileSync(process.execPath, [resolve(root, 'scripts/build/macos-package.mjs')], { stdio: 'inherit', env: { ...process.env, PERO_PRODUCT_PAYLOAD: output } });
