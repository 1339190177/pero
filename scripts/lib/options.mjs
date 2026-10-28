import { resolve } from 'node:path';
import { parseArgs } from 'node:util';
import { root } from './paths.mjs';

// Build-tool paths use its working directory; npm defaults use the repository.
export function scriptOptions(defaultOutput, extraOptions = {}) {
  const { values } = parseArgs({ options: {
    root: { type: 'string' },
    check: { type: 'boolean', default: false },
    ...(defaultOutput ? { 'out-dir': { type: 'string' } } : {}),
    ...extraOptions,
  } });
  const sourceRoot = resolve(values.root ?? root);
  return {
    ...values,
    root: sourceRoot,
    ...(defaultOutput ? { outDir: resolve(values['out-dir'] ?? resolve(sourceRoot, defaultOutput)) } : {}),
  };
}
