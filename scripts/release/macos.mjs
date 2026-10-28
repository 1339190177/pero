// CI-owned release metadata and packaged smoke. Publication is the workflow's final step.
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { basename, join, resolve } from 'node:path';

const root = resolve(import.meta.dirname, '../..');
const taskCenter = 'https://app.openpero.com';
const kitRevision = 'ec89cfc36404667b7947aeb3d40b1c2859d729dd';
const tag = process.env.RELEASE_TAG || '';
if (process.env.GITHUB_ACTIONS !== 'true' || process.platform !== 'darwin' || process.arch !== 'arm64') {
  throw new Error('Release packages must be built on a GitHub Actions macOS arm64 runner.');
}
if (!/^v\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?$/.test(tag)) throw new Error('RELEASE_TAG must identify an existing version tag.');
const git = (...args) => execFileSync('git', args, { cwd: root, encoding: 'utf8' }).trim();
const revision = git('rev-parse', 'HEAD');
if (git('rev-parse', `refs/tags/${tag}^{commit}`) !== revision) throw new Error('The release tag must match the checked out source.');
const packageDir = resolve(root, 'target/package');
const app = join(packageDir, 'Pero.app');
const payload = join(app, 'Contents/Resources/app');
const metadataPath = resolve(root, 'desktop/dist/release.json');
const outputDir = resolve(root, 'test-results/release');
mkdirSync(outputDir, { recursive: true });
const operation = process.argv[2];

if (operation === 'prepare') {
  const actualKit = execFileSync('git', ['-C', process.env.PERO_GPUI_KIT_PATH, 'rev-parse', 'HEAD'], { encoding: 'utf8' }).trim();
  if (actualKit !== kitRevision) throw new Error('The release requires the pinned GPUI snapshot.');
  const product = JSON.parse(readFileSync(resolve(root, 'products/pero.json'), 'utf8'));
  const metadata = {
    schemaVersion: 1, releaseTag: tag, productVersion: product.version, sourceRevision: revision,
    taskCenter, platform: 'darwin', arch: 'arm64', gpuiKitRevision: kitRevision,
    buildProvider: 'GitHub Actions',
    buildURL: `https://github.com/${process.env.GITHUB_REPOSITORY}/actions/runs/${process.env.GITHUB_RUN_ID}`,
    signing: 'ad-hoc', notarized: false, bundledAgentCLIs: false,
  };
  mkdirSync(resolve(root, 'desktop/dist'), { recursive: true });
  writeFileSync(metadataPath, JSON.stringify(metadata, null, 2) + '\n');
} else if (operation === 'verify') {
  const metadata = JSON.parse(readFileSync(join(payload, 'desktop/dist/release.json'), 'utf8'));
  if (metadata.sourceRevision !== revision || metadata.releaseTag !== tag || metadata.taskCenter !== taskCenter || metadata.buildProvider !== 'GitHub Actions') {
    throw new Error('Packaged release metadata does not match this workflow.');
  }
  const main = readFileSync(join(payload, 'desktop/dist/main.cjs'), 'utf8');
  if (!main.includes(taskCenter) || main.includes('tests/fixtures') || main.includes('contribution-fixture')) {
    throw new Error('The packaged Desktop must use the online service and exclude fixture code.');
  }
  for (const name of ['tests', 'test-results', '.git', '.env', '.env.local', '.codex', '.claude', 'agents.local.json']) {
    if (existsSync(join(payload, name))) throw new Error(`Unexpected private or test input in package: ${name}`);
  }
  for (const executable of ['Contents/MacOS/Pero', 'Contents/Resources/app/bin/pero-host']) {
    const arch = execFileSync('/usr/bin/lipo', ['-archs', join(app, executable)], { encoding: 'utf8' }).trim();
    if (arch !== 'arm64') throw new Error(`Unexpected packaged architecture: ${arch}`);
  }
  execFileSync('/usr/bin/codesign', ['--verify', '--deep', '--strict', app], { stdio: 'inherit' });
  const size = JSON.parse(readFileSync(join(packageDir, 'size.json'), 'utf8'));
  const dmg = join(packageDir, 'Pero-macos-arm64.dmg');
  const images = readdirSync(packageDir).filter(name => name.endsWith('.dmg'));
  if (images.length !== 1 || images[0] !== basename(dmg) || !existsSync(dmg) || size.dmgBytes >= 100_000_000) throw new Error('Unexpected or oversized DMG output.');
  const sha256 = path => createHash('sha256').update(readFileSync(path)).digest('hex');
  const releaseMetadata = { ...metadata, dmg: basename(dmg), dmgBytes: size.dmgBytes, dmgSha256: sha256(dmg) };
  const releasedMetadata = join(packageDir, 'Pero-macos-arm64.build.json');
  writeFileSync(releasedMetadata, JSON.stringify(releaseMetadata, null, 2) + '\n');
  writeFileSync(join(packageDir, 'SHA256SUMS'), [dmg, releasedMetadata].map(path => `${sha256(path)}  ${basename(path)}\n`).join(''));
} else if (operation === 'smoke') {
  const { _electron: electron, expect } = await import('@playwright/test');
  const scratch = mkdtempSync(join(tmpdir(), 'pero-release-'));
  const env = { ...process.env, PERO_DESKTOP_DATA_DIR: join(scratch, 'profile'), PERO_SETTINGS_PATH: join(scratch, 'settings.json'), PERO_HISTORY_PATH: join(scratch, 'history'), PERO_LOCALE: 'en' };
  for (const key of ['ELECTRON_RUN_AS_NODE', 'PERO_CLOUD_URL', 'PERO_OPERATOR_TOKEN', 'PERO_CLIENT_TOKEN', 'PERO_AGENTS_FILE', 'PERO_AGENT_ROOT', 'PERO_HOST_PATH', 'GH_TOKEN', 'GITHUB_TOKEN']) delete env[key];
  let desktop;
  try {
    desktop = await electron.launch({ executablePath: join(app, 'Contents/MacOS/Pero'), args: ['--use-mock-keychain'], cwd: scratch, env });
    const page = await desktop.firstWindow();
    const errors = []; page.on('pageerror', error => errors.push(error.message));
    await page.waitForFunction(() => window.peroDiagnostics, undefined, { timeout: 60_000 });
    const snapshot = () => page.evaluate(() => JSON.parse(window.peroDiagnostics.snapshot()));
    await expect.poll(async () => (await snapshot()).ready, { timeout: 60_000 }).toBe(true);
    await expect.poll(async () => (await snapshot()).connected, { timeout: 60_000 }).toBe(true);
    const state = await snapshot();
    expect(state.remote.state.connection.url).toBe(taskCenter);
    expect(state.remote.state.configured).toBe(false);
    expect(errors).toEqual([]);
    await page.screenshot({ path: join(outputDir, 'packaged-start.png') });
    writeFileSync(join(outputDir, 'smoke.json'), JSON.stringify({ result: 'passed', releaseTag: tag, sourceRevision: revision, taskCenter: state.remote.state.connection.url, hostReady: state.connected, errors }, null, 2) + '\n');
  } finally {
    if (desktop) await desktop.close();
    rmSync(scratch, { recursive: true, force: true });
  }
} else {
  throw new Error('Use prepare, verify or smoke.');
}
