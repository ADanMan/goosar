// Детерминированный хэш содержимого собранного дерева артефакта.

import { readdir, readlink } from 'node:fs/promises';
import { createReadStream } from 'node:fs';
import { createHash } from 'node:crypto';
import { join, relative, sep } from 'node:path';

export const HASH_EXCLUDED_NAMES = new Set(['ARTIFACT-MANIFEST.txt', 'ARTIFACT-NOT-BUNDLED.txt']);

async function listArtifactEntries(dir, base = dir, out = []) {
  const entries = await readdir(dir, { withFileTypes: true });
  for (const entry of entries) {
    const full = join(dir, entry.name);
    if (entry.isSymbolicLink()) {
      out.push({ type: 'link', full });
    } else if (entry.isDirectory()) {
      await listArtifactEntries(full, base, out);
    } else {
      out.push({ type: 'file', full });
    }
  }
  return out;
}

function hashFile(path) {
  return new Promise((resolvePromise, reject) => {
    const hash = createHash('sha256');
    const stream = createReadStream(path);
    stream.on('data', (chunk) => hash.update(chunk));
    stream.on('end', () => resolvePromise(hash.digest('hex')));
    stream.on('error', reject);
  });
}

export async function hashArtifactTree(root) {
  const entries = await listArtifactEntries(root);
  const withRelPaths = entries
    .map((entry) => ({
      ...entry,
      rel: relative(root, entry.full).split(sep).join('/'),
    }))
    .filter((entry) => !HASH_EXCLUDED_NAMES.has(entry.rel));
  withRelPaths.sort((a, b) => (a.rel < b.rel ? -1 : a.rel > b.rel ? 1 : 0));

  const summary = createHash('sha256');
  for (const entry of withRelPaths) {
    if (entry.type === 'link') {
      const target = await readlink(entry.full);
      summary.update(`L ${entry.rel} -> ${target}\n`);
    } else {
      const fileHash = await hashFile(entry.full);
      summary.update(`F ${entry.rel} ${fileHash}\n`);
    }
  }
  return summary.digest('hex');
}
