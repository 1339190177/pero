import { cargoArgs } from './lib/cargo.mjs';
import { run } from './lib/run.mjs';

await run('cargo', cargoArgs(process.argv.slice(2)));
