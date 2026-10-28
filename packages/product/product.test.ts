import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Product } from './product.ts';
const base = { schemaVersion: 1, id: 'com.openpero.virtualcell', name: 'Virtual Cell', version: '0.2.0', shell: 'app', apps: ['virtual-cell'], entry: { app: 'virtual-cell', panel: 'main' }, python: 'bundled' };
test('Product delivery invariants', () => {
  for (const [label, value, accepted] of [
    ['standalone entry', base, true],
    ['workbench', { ...base, shell: 'workbench', entry: undefined }, true],
    ['entry excluded', { ...base, apps: [] }, false],
    ['missing panel', { ...base, entry: { app: 'virtual-cell' } }, false],
    ['duplicate Apps', { ...base, apps: ['virtual-cell', 'virtual-cell'] }, false],
    ['unsafe name', { ...base, name: '../Escape' }, false],
    ['unknown shell', { ...base, shell: 'custom' }, false],
    ['unknown runtime', { ...base, python: 'download' }, false],
    ['unknown field', { ...base, command: 'anything' }, false],
    ['future version', { ...base, schemaVersion: 2 }, false],
  ] as const) {
    if (accepted) assert.doesNotThrow(() => Product.parse(value), label);
    else assert.throws(() => Product.parse(value), undefined, label);
  }
});
