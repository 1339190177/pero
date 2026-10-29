// Managed fontTools environments are disposable, including restored target caches.
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { run } from '../lib/run.mjs';

export function fontToolsPython(directory, platform = process.platform) {
  return join(directory, platform === 'win32' ? 'Scripts/python.exe' : 'bin/python');
}

export async function prepareFontTools({ directory, version, python = process.env.PERO_PYTHON || 'python3', env = process.env }) {
  const probe = [
    'import fontTools',
    'from fontTools import subset',
    'from fontTools.ttLib import TTFont',
    'from fontTools.varLib.instancer import instantiateVariableFont',
    `assert fontTools.__version__ == ${JSON.stringify(version)}`,
  ].join('; ');
  const hasTool = (executable, environment) =>
    spawnSync(executable, ['-c', probe], { env: environment, stdio: 'ignore' }).status === 0;
  if (hasTool(python, env)) return { python, env };
  const executable = fontToolsPython(directory);
  const isolated = { ...env }; delete isolated.PYTHONPATH;
  if (!hasTool(executable, isolated)) {
    // An existing interpreter says nothing about pip/package completeness. Clear
    // damaged cached files and metadata together; ensurepip alone may skip them.
    await run(python, ['-m', 'venv', '--clear', directory], { env: isolated });
    await run(executable, ['-m', 'pip', 'install', '--disable-pip-version-check', '--no-deps', `fonttools==${version}`], { env: isolated });
    if (!hasTool(executable, isolated)) throw new Error('Pinned fontTools setup failed');
  }
  return { python: executable, env: isolated };
}
