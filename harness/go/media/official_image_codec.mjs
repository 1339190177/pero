// Point's bytes-only process carrier for the deployed official attachment codec.
// The actual normalization and request transforms are public upstream exports.
import { createRequire } from 'node:module';
import { createHash } from 'node:crypto';
import { readFileSync, realpathSync } from 'node:fs';
import { dirname, resolve, sep } from 'node:path';
import { pathToFileURL } from 'node:url';

const [anchor, scratch, expectedIdentity] = process.argv.slice(1);
const require = createRequire(anchor);
const modulePath = realpathSync(require.resolve('@deepseek-ai/dsh-attachment-local'));
const packagePath = realpathSync(require.resolve('@deepseek-ai/dsh-attachment-local/package.json'));
const manifest = JSON.parse(readFileSync(packagePath, 'utf8'));
// The public codec implementation is unchanged between these audited releases.
// Keep an exact allowlist; later releases require their own ABI/oracle review.
const supportedVersions = new Set(['0.2.1-alpha.2', '0.2.1-alpha.3']);
if (manifest.name !== '@deepseek-ai/dsh-attachment-local' || !supportedVersions.has(manifest.version) || !modulePath.startsWith(dirname(packagePath) + sep)) {
  throw new Error('The configured installation does not provide the official attachment package.');
}
const sharp = createRequire(modulePath)('sharp');
const identity = createHash('sha256').update(JSON.stringify({
  carrier: 1, package: manifest.name, version: manifest.version,
  module: createHash('sha256').update(readFileSync(modulePath)).digest('hex'),
  versions: sharp.versions,
})).digest('hex');
if (expectedIdentity && identity !== expectedIdentity) throw new Error('The installed image codec changed after binding.');
const codec = await import(pathToFileURL(modulePath));
for (const name of ['prepareImageFile', 'readRequestImageFile', 'requestImageVariantId']) {
  if (typeof codec[name] !== 'function') throw new Error('The configured attachment codec is missing a required public export.');
}
const input = JSON.parse(readFileSync(0, 'utf8'));
try {
  let result;
  if (input.operation === 'identify') {
    result = { identity, version: manifest.version, versions: sharp.versions };
  } else if (input.operation === 'prepare') {
    const prepared = await codec.prepareImageFile({
      data: Buffer.from(input.data, 'base64'), mediaType: input.mediaType, name: input.name,
    }, input.limits, input.policy);
    result = { identity, ref: prepared.ref, data: Buffer.from(prepared.data).toString('base64') };
  } else if (input.operation === 'request') {
    // The root is a fresh mode-0700 Go-owned scratch directory. No caller path
    // or durable home is exposed; Go owns the final content-addressed commit.
    const version = await codec.readRequestImageFile(resolve(scratch, 'cache'), {
      ref: input.ref, data: Buffer.from(input.data, 'base64'),
    }, input.target);
    result = { ...version, identity, data: Buffer.from(version.data).toString('base64') };
  } else {
    throw new Error('Unknown image codec operation.');
  }
  process.stdout.write(JSON.stringify(result));
} catch (error) {
  process.stdout.write(JSON.stringify({ identity, error: { code: error.code ?? 'ATTACHMENT_WRITE_FAILED', message: String(error.message) } }));
}
