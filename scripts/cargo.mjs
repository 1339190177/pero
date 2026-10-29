import { cargoArgs, ensureGpuiLock } from './lib/cargo.mjs';
import { run } from './lib/run.mjs';

const args = process.argv.slice(2);
if (args.includes('--locked')) await ensureGpuiLock();
await run('cargo', cargoArgs(args));
