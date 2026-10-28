import { spawn } from 'node:child_process';
import { root } from './paths.mjs';

export function run(command, args, options = {}) {
  return new Promise((resolveRun, reject) => {
    const child = spawn(command, args, { cwd: root, stdio: 'inherit', ...options });
    child.on('error', reject);
    child.on('exit', (code, signal) => code === 0 ? resolveRun() : reject(new Error(`${command} failed (${code ?? signal})`)));
  });
}
