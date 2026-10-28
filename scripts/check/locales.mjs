import { readFileSync, readdirSync } from 'node:fs';
import { resolve } from 'node:path';
import assert from 'node:assert/strict';
import { scriptOptions } from '../lib/options.mjs';
const { root, source } = scriptOptions(undefined, { source: { type: 'string', multiple: true } });
const en = JSON.parse(readFileSync(resolve(root, 'locales/en.json'), 'utf8'));
const zh = JSON.parse(readFileSync(resolve(root, 'locales/zh-CN.json'), 'utf8'));
assert.deepEqual(Object.keys(en).sort(), Object.keys(zh).sort(), 'Catalog key parity');
const placeholders = value => [...value.matchAll(/\{(\w+)\}/g)].map(match => match[1]).sort();
for (const key of Object.keys(en)) {
  assert.ok(en[key].trim() && zh[key].trim(), `Empty translation: ${key}`);
  assert.deepEqual(placeholders(en[key]), placeholders(zh[key]), `Interpolation mismatch: ${key}`);
}
const files = source ?? readdirSync(resolve(root, 'crates/pero-ui/src'))
  .filter(file => file.endsWith('.rs')).sort().map(file => `crates/pero-ui/src/${file}`);
for (const file of files) {
  const content = readFileSync(resolve(root, file), 'utf8');
  for (const [, key] of content.matchAll(/"((?:nav|task|tasks|status|overview|chat|agents|filter|time|error|probe|tab|field|appearance|workspace|input|workbench|github|menu|verification|projects|environment)\.[a-z_\.]+)"/g)) {
    assert.ok(key in en, `${file}: missing translation ${key}`);
  }
}
console.log(`Checked ${Object.keys(en).length} bilingual messages and UI references.`);
