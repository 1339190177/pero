import { readFileSync, writeFileSync, mkdirSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { execFileSync } from 'node:child_process';
import { compile } from 'json-schema-to-typescript';
import { root } from '../lib/paths.mjs';

// TypeSpec emits one $id per type; rewrite to a self-contained $defs bundle that
// ajv, json-schema-to-typescript, typify and datamodel-code-generator all read.
const emit = (name) => {
  const temp = mkdtempSync(`${tmpdir()}/pero-contracts-`);
  try {
    execFileSync(`${root}/node_modules/.bin/tsp`, ['compile', `${root}/protocol/${name}.tsp`,
      '--emit', '@typespec/json-schema', '--output-dir', temp, '--warn-as-error',
      '--option', '@typespec/json-schema.file-type=json',
      '--option', `@typespec/json-schema.bundleId=${name}.schema.json`], { stdio: 'pipe' });
    return JSON.parse(readFileSync(`${temp}/@typespec/json-schema/${name}.schema.json`, 'utf8'));
  } catch (error) {
    process.stderr.write(`${error.stdout ?? ''}${error.stderr ?? ''}`);
    throw new Error('TypeSpec compilation failed');
  } finally { rmSync(temp, { recursive: true, force: true }); }
};

// `...Record<never>` emits `{ not: {} }`, i.e. no further properties are allowed.
const closed = (value) => JSON.stringify(value) === '{"not":{}}';
const normalize = (node) => {
  if (Array.isArray(node)) return node.map(normalize);
  if (!node || typeof node !== 'object') return node;
  const out = {};
  for (const [key, value] of Object.entries(node)) {
    if (key === '$schema' || key === '$id') continue;
    if (key === '$ref' && typeof value === 'string') out.$ref = value.startsWith('#/') ? value : `#/$defs/${value.replace(/\.json$/, '')}`;
    // Models are spread, never composed, so unevaluated and additional properties agree.
    else if (key === 'unevaluatedProperties') out.additionalProperties = closed(value) ? false : normalize(value);
    else out[key] = normalize(value);
  }
  return out;
};
async function generate(name, title, roots) {
const emitted = emit(name);
const $defs = Object.fromEntries(Object.entries(emitted.$defs).sort(([a], [b]) => a.localeCompare(b))
  .map(([name, schema]) => [name, { title: name, ...normalize(schema) }]));
const schema = {
  $schema: 'https://json-schema.org/draft/2020-12/schema',
  $id: `https://openpero.com/protocol/${name}.schema.json`,
  title,
  description: `Generated from protocol/${name}.tsp. Do not edit.`,
  oneOf: roots.map(type => ({ $ref: `#/$defs/${type}` })),
  $defs,
};
const types = await compile(structuredClone(schema), title, {
  additionalProperties: false, bannerComment: `/* Generated from protocol/${name}.tsp by scripts/codegen/contracts.mjs. Do not edit. */`,
  strictIndexSignatures: true, unreachableDefinitions: true, format: true,
});

const outputs = {
  [`${root}/protocol/generated/${name}.schema.json`]: JSON.stringify(schema, null, 2) + '\n',
  [`${root}/protocol/generated/${name}.ts`]: types,
};
mkdirSync(`${root}/protocol/generated`, { recursive: true });
for (const [path, output] of Object.entries(outputs)) {
  if (process.argv.includes('--check')) {
    let current = '';
    try { current = readFileSync(path, 'utf8'); } catch {}
    if (current !== output) throw new Error(`${path} is stale. Run npm run gen:contracts`);
  } else writeFileSync(path, output);
}
}
await generate('host', 'HostProtocol', ['HostInput', 'HostEvent']);
await generate('remote', 'RemoteProtocol', ['RemoteProtocol']);
