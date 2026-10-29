import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, rm, writeFile, stat } from 'node:fs/promises';
import { tmpdir, devNull } from 'node:os';
import { join } from 'node:path';
import { execFileSync } from 'node:child_process';
import { fontToolsPython, prepareFontTools } from './fonttools.mjs';

const python = process.env.PERO_PYTHON || 'python3';
const version = '0.0.1'; // Local fixture, never a replacement for the production pin.

test('fontTools interpreter location follows the target platform', () => {
  assert.equal(fontToolsPython('tool', 'win32'), join('tool', 'Scripts/python.exe'));
  for (const platform of ['linux', 'darwin']) {
    assert.equal(fontToolsPython('tool', platform), join('tool', 'bin/python'));
  }
});

test('fontTools bootstrap recovers incomplete environments and reuses healthy ones offline', async t => {
  const temp = await mkdtemp(join(tmpdir(), 'pero-fonttools-'));
  t.after(() => rm(temp, { recursive: true, force: true }));
  const wheels = join(temp, 'wheels');
  await mkdir(wheels);
  // A deliberately tiny wheel tests installation/recovery without downloading
  // fonts or packages. The imports match fonts.py's runtime prerequisites.
  execFileSync(python, ['-c', `
import sys, zipfile
from pathlib import Path
files = {
 'fontTools/__init__.py': '__version__ = "0.0.1"',
 'fontTools/subset.py': '',
 'fontTools/ttLib/__init__.py': 'class TTFont: pass',
 'fontTools/varLib/__init__.py': '',
 'fontTools/varLib/instancer.py': 'def instantiateVariableFont(): pass',
 'fonttools-0.0.1.dist-info/METADATA': 'Metadata-Version: 2.1\\nName: fonttools\\nVersion: 0.0.1\\n',
 'fonttools-0.0.1.dist-info/WHEEL': 'Wheel-Version: 1.0\\nGenerator: fixture\\nRoot-Is-Purelib: true\\nTag: py3-none-any\\n',
}
files['fonttools-0.0.1.dist-info/RECORD'] = ''.join(name + ',,\\n' for name in files)
with zipfile.ZipFile(Path(sys.argv[1]) / 'fonttools-0.0.1-py3-none-any.whl', 'w') as wheel:
 for name, data in files.items(): wheel.writestr(name, data)
`, wheels]);
  const env = { ...process.env, PIP_CONFIG_FILE: devNull, PIP_NO_INDEX: '1', PIP_NO_CACHE_DIR: '1', PIP_FIND_LINKS: wheels };
  const directory = join(temp, 'managed');
  const executable = fontToolsPython(directory);
  const prepare = () => prepareFontTools({ directory, version, python, env });
  let site;
  for (const state of ['missing', 'missing pip entry point', 'missing fontTools submodule', 'interrupted creation']) {
    await t.test(state, async () => {
      if (state === 'missing pip entry point') {
        await rm(join(site, 'pip', '__main__.py'));
        await rm(join(site, 'fontTools'), { recursive: true });
      } else if (state === 'missing fontTools submodule') {
        await rm(join(site, 'fontTools', 'varLib', 'instancer.py'));
      } else if (state === 'interrupted creation') {
        await rm(directory, { recursive: true });
        execFileSync(python, ['-m', 'venv', '--without-pip', directory]);
      }
      assert.equal((await prepare()).python, executable);
      site = execFileSync(executable, ['-c', 'import sysconfig; print(sysconfig.get_path("purelib"))'], { encoding: 'utf8' }).trim();
      execFileSync(executable, ['-m', 'pip', '--version']);
      execFileSync(executable, ['-c', 'from fontTools.varLib.instancer import instantiateVariableFont']);
    });
  }
  await t.test('healthy tool needs no reinstall or package index', async () => {
    const marker = join(directory, 'keep-me');
    await writeFile(marker, 'healthy');
    const before = await stat(join(site, 'fontTools', '__init__.py'));
    await rm(wheels, { recursive: true });
    await prepare();
    assert.equal((await stat(join(site, 'fontTools', '__init__.py'))).mtimeMs, before.mtimeMs);
    assert.ok(await stat(marker));
  });
  await t.test('installation failure is propagated, not accepted as ready', async () => {
    await rm(join(site, 'fontTools'), { recursive: true });
    await assert.rejects(prepare(), /failed/);
  });
});
