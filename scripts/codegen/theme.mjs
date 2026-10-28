import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { parse } from 'yaml';
import { root } from '../lib/paths.mjs';
const source = readFileSync(`${root}/DESIGN.md`, 'utf8').split('---')[1];
const { light, dark } = parse(source).colors;
const variables = colors => Object.entries(colors).map(([name, value]) => `--${name}:${value};`).join('');
const outputs = [
  ['web/src/theme.generated.json', JSON.stringify({ light, dark }, null, 2) + '\n'],
  ['server/internal/community/assets/theme.generated.css',
    `/* Generated from DESIGN.md; run npm run gen:theme. */\n:root{color-scheme:light dark;${variables(light)}}\n@media(prefers-color-scheme:dark){:root{${variables(dark)}}}\n`],
];
for (const [relative, output] of outputs) {
  const path = `${root}/${relative}`;
  if (process.argv.includes('--check')) {
    if (readFileSync(path, 'utf8') !== output) throw new Error('Run npm run gen:theme');
  } else {
    mkdirSync(path.slice(0, path.lastIndexOf('/')), { recursive: true });
    writeFileSync(path, output);
  }
}
