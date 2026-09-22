import { promises as fs } from 'fs';
import { join } from 'path';

import { hashArtifactTree } from '../../scripts/hash-artifact-tree.mjs';

export async function readBundleManifest(
  bundledDir: string,
  manifestFile: string,
  keyPrefix: 'server' | 'entry',
): Promise<Map<string, string>> {
  const out = new Map<string, string>();
  let raw: string;
  try {
    raw = await fs.readFile(join(bundledDir, manifestFile), 'utf-8');
  } catch {
    return out;
  }
  const lineRe = new RegExp(`^${keyPrefix}=(\\S+)\\s+sha256=([0-9a-f]+)`);
  for (const line of raw.split('\n')) {
    const match = lineRe.exec(line.trim());
    if (match) out.set(match[1], match[2]);
  }
  return out;
}

export interface EntryVerification {
  ok: boolean;
  reason?: string;
}

export async function verifyBundledEntry(
  bundledDir: string,
  name: string,
  manifest: Map<string, string>,
): Promise<EntryVerification> {
  const expected = manifest.get(name);
  if (!expected) {
    return { ok: false, reason: 'no manifest entry covers it' };
  }
  let actual: string;
  try {
    actual = await hashArtifactTree(join(bundledDir, name));
  } catch (err) {
    return {
      ok: false,
      reason: `could not hash the staged tree: ${err instanceof Error ? err.message : String(err)}`,
    };
  }
  if (actual !== expected) {
    return {
      ok: false,
      reason: `sha256 mismatch (manifest ${expected.slice(0, 12)}…, tree ${actual.slice(0, 12)}…)`,
    };
  }
  return { ok: true };
}
