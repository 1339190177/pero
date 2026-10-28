/** Product is immutable delivery intent. App manifests continue to own domain features. */
export type ProductFields = {
  schemaVersion: 1; id: string; name: string; version: string;
  shell: 'workbench' | 'app'; apps: readonly string[];
  entry?: Readonly<{ app: string; panel: string }>;
  python: 'system' | 'bundled';
};
export class Product {
  readonly schemaVersion = 1;
  readonly id: string;
  readonly name: string;
  readonly version: string;
  readonly shell: 'workbench' | 'app';
  readonly apps: readonly string[];
  readonly entry?: Readonly<{ app: string; panel: string }>;
  readonly python: 'system' | 'bundled';

  private constructor(fields: ProductFields) {
    this.id = fields.id; this.name = fields.name; this.version = fields.version;
    this.shell = fields.shell; this.apps = Object.freeze([...fields.apps]);
    this.entry = fields.entry ? Object.freeze({ ...fields.entry }) : undefined;
    this.python = fields.python;
    Object.freeze(this);
  }

  static parse(value: unknown): Product {
    const v = value as ProductFields;
    const slug = (s: unknown): s is string => typeof s === 'string' && /^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(s);
    if (!v || typeof v !== 'object' || Array.isArray(v) ||
      Object.keys(v).some(key => !['schemaVersion', 'id', 'name', 'version', 'shell', 'apps', 'entry', 'python'].includes(key)) ||
      v.schemaVersion !== 1 || typeof v.id !== 'string' || !/^[a-z][a-z0-9]*(?:\.[a-z][a-z0-9-]*){2,}$/.test(v.id) ||
      typeof v.name !== 'string' || !/^[\p{L}\p{N}][\p{L}\p{N} ._-]{0,63}$/u.test(v.name) ||
      typeof v.version !== 'string' || !/^\d+\.\d+\.\d+$/.test(v.version) ||
      !['workbench', 'app'].includes(v.shell) || !['system', 'bundled'].includes(v.python) ||
      !Array.isArray(v.apps) || !v.apps.every(slug) || new Set(v.apps).size !== v.apps.length) {
      throw new Error('Invalid Product definition');
    }
    if (v.shell === 'app') {
      if (!v.entry || Object.keys(v.entry).some(key => !['app', 'panel'].includes(key)) ||
        !slug(v.entry.app) || !slug(v.entry.panel) || !v.apps.includes(v.entry.app)) {
        throw new Error('App Product requires an entry panel in its included Apps');
      }
    } else if (v.entry !== undefined) throw new Error('Workbench Product has no fixed entry');
    return new Product(v);
  }
}
