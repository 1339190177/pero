import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { parse } from 'yaml';
import { root } from '../lib/paths.mjs';
const source = readFileSync(`${root}/DESIGN.md`, 'utf8').split('---')[1];
const { light, dark } = parse(source).colors;
const output = JSON.stringify({ light, dark }, null, 2) + '\n';
const path = `${root}/web/src/theme.generated.json`;
mkdirSync(`${root}/web/src`, { recursive: true });
if (process.argv.includes('--check')) {
  if (readFileSync(path, 'utf8') !== output) throw new Error('Run npm run gen:theme');
} else writeFileSync(path, output);
