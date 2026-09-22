#!/usr/bin/env node
import { execFileSync } from 'node:child_process';
import { lstatSync, readdirSync, readlinkSync, existsSync } from 'node:fs';
import { basename, join, posix, relative } from 'node:path';

export function walkTree(root) {
  const inventory = new Map();
  const visit = (absDir) => {
    for (const name of readdirSync(absDir).sort()) {
      const abs = join(absDir, name);
      const rel = relative(root, abs).split('\\').join('/');
      const st = lstatSync(abs);
      if (st.isSymbolicLink()) {
        inventory.set(rel, {
          kind: 'link',
          size: 0,
          target: readlinkSync(abs).split('\\').join('/'),
        });
      } else if (st.isDirectory()) {
        inventory.set(rel, { kind: 'dir', size: 0, target: null });
        visit(abs);
      } else {
        inventory.set(rel, { kind: 'file', size: st.size, target: null });
      }
    }
  };
  visit(root);
  return inventory;
}

export function resolvePathInInventory(inventory, relPath, maxHops = 64) {
  let remaining = relPath.split('/').filter(Boolean);
  const resolved = [];
  for (let hops = 0; remaining.length > 0; hops += 1) {
    if (hops > maxHops) return null; 
    const segment = remaining.shift();
    if (segment === '.') continue;
    if (segment === '..') {
      if (resolved.length === 0) return { kind: 'external', size: 0, target: relPath };
      resolved.pop();
      continue;
    }
    const candidate = [...resolved, segment].join('/');
    const entry = inventory.get(candidate);
    if (!entry) return null;
    if (entry.kind === 'link') {
      const target = entry.target ?? '';
      if (target.startsWith('/')) return { kind: 'external', size: 0, target };
      remaining = [...target.split('/').filter(Boolean), ...remaining];
      continue;
    }
    resolved.push(segment);
    if (remaining.length === 0) return entry;
  }
  return { kind: 'dir', size: 0, target: null };
}

export function findDanglingSymlinks(inventory) {
  const dangling = [];
  for (const [rel, entry] of inventory) {
    if (entry.kind !== 'link') continue;
    if (resolvePathInInventory(inventory, rel) === null) dangling.push(rel);
  }
  return dangling.sort();
}

export function compareInventories(source, artifact, { strictSymlinks = true } = {}) {
  const problems = [];
  for (const [rel, want] of source) {
    const got = artifact.get(rel);
    if (!got) {
      problems.push(
        `MISSING in artifact: ${rel} (${want.kind}${want.kind === 'file' ? `, ${want.size} bytes` : ''})`,
      );
      continue;
    }
    if (want.kind === 'file' && got.kind === 'file' && want.size !== got.size) {
      problems.push(
        `SIZE MISMATCH: ${rel} — source ${want.size} bytes, artifact ${got.size} bytes`,
      );
    }
    if (
      strictSymlinks &&
      want.kind === 'link' &&
      got.kind === 'link' &&
      want.target !== got.target
    ) {
      problems.push(
        `SYMLINK TARGET CHANGED: ${rel} — source "${want.target}", artifact "${got.target}"`,
      );
    }
    if (strictSymlinks && want.kind !== got.kind) {
      problems.push(`KIND CHANGED: ${rel} — source ${want.kind}, artifact ${got.kind}`);
    }
  }
  if (strictSymlinks) {
    for (const rel of findDanglingSymlinks(artifact)) {
      problems.push(`DANGLING SYMLINK in artifact: ${rel} -> ${artifact.get(rel)?.target ?? '?'}`);
    }
  }
  return problems;
}

export function attachDmgReadOnly(dmgPath) {
  const out = execFileSync(
    'hdiutil',
    ['attach', '-readonly', '-nobrowse', '-noverify', '-mountrandom', '/tmp', dmgPath],
    { encoding: 'utf-8' },
  );
  const mounts = out
    .split('\n')
    .map((line) => line.split('\t').pop()?.trim() ?? '')
    .filter((p) => p.startsWith('/'));
  const mountPoint = mounts.pop();
  if (!mountPoint) throw new Error(`could not determine mount point for ${dmgPath}`);
  return mountPoint;
}

export function detachDmg(mountPoint) {
  try {
    execFileSync('hdiutil', ['detach', mountPoint, '-quiet'], { stdio: 'ignore' });
  } catch {
    try {
      execFileSync('hdiutil', ['detach', mountPoint, '-force', '-quiet'], { stdio: 'ignore' });
    } catch {
      // Nothing more we can do; surface nothing so the real verification
      // error (if any) is what the caller reports.
    }
  }
}

export function verifyDmgArtifact(sourceApp, dmgPath) {
  const appName = basename(sourceApp);
  const source = walkTree(sourceApp);
  const mountPoint = attachDmgReadOnly(dmgPath);
  try {
    const insideApp = join(mountPoint, appName);
    if (!existsSync(insideApp)) {
      return { ok: false, problems: [`MISSING in artifact: ${appName} is not on the image`] };
    }
    const problems = compareInventories(source, walkTree(insideApp));
    return { ok: problems.length === 0, problems };
  } finally {
    detachDmg(mountPoint);
  }
}

export function listZipInventory(zipPath, appName) {
  const out = execFileSync('unzip', ['-l', zipPath], {
    encoding: 'utf-8',
    maxBuffer: 64 * 1024 * 1024,
  });
  const inventory = new Map();
  const prefix = `${appName}/`;
  for (const line of out.split('\n')) {
    const m = line.match(/^\s*(\d+)\s+\S+\s+\S+\s+(.+)$/);
    if (!m) continue;
    const name = m[2].trim();
    if (!name.startsWith(prefix)) continue;
    const rel = name.slice(prefix.length).replace(/\/$/, '');
    if (!rel) continue;
    inventory.set(rel, {
      kind: name.endsWith('/') ? 'dir' : 'file',
      size: Number(m[1]),
      target: null,
    });
  }
  return inventory;
}

export function verifyZipArtifact(sourceApp, zipPath) {
  const appName = basename(sourceApp);
  const source = walkTree(sourceApp);
  const artifact = listZipInventory(zipPath, appName);
  if (artifact.size === 0) {
    return {
      ok: false,
      problems: [`MISSING in artifact: no ${appName}/ entries in ${basename(zipPath)}`],
    };
  }
  const comparable = new Map([...source].filter(([, e]) => e.kind !== 'dir'));
  const problems = compareInventories(comparable, artifact, { strictSymlinks: false });
  return { ok: problems.length === 0, problems };
}

function parseArgv(argv) {
  const parsed = { source: null, dmg: null, zip: null };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === '--source') parsed.source = argv[++i];
    else if (arg === '--dmg') parsed.dmg = argv[++i];
    else if (arg === '--zip') parsed.zip = argv[++i];
    else throw new Error(`unknown option: ${arg}`);
  }
  return parsed;
}

function main() {
  let parsed;
  try {
    parsed = parseArgv(process.argv.slice(2));
  } catch (err) {
    console.error(`[verify-artifact] ${err.message}`);
    process.exit(1);
  }
  if (!parsed.source || (!parsed.dmg && !parsed.zip)) {
    console.error('[verify-artifact] usage: --source <App.app> (--dmg <file> | --zip <file>)');
    process.exit(1);
  }
  const artifactPath = parsed.dmg ?? parsed.zip;
  let result;
  try {
    result = parsed.dmg
      ? verifyDmgArtifact(parsed.source, parsed.dmg)
      : verifyZipArtifact(parsed.source, parsed.zip);
  } catch (err) {
    console.error(`[verify-artifact] FAILED to inspect ${basename(artifactPath)}: ${err.message}`);
    process.exit(1);
  }
  if (!result.ok) {
    console.error(
      `[verify-artifact] ${basename(artifactPath)} is INCOMPLETE — refusing to ship it (issue #186):`,
    );
    for (const problem of result.problems.slice(0, 25)) console.error(`    ${problem}`);
    if (result.problems.length > 25) {
      console.error(`    … and ${result.problems.length - 25} more`);
    }
    process.exit(1);
  }
  console.log(
    `[verify-artifact] ${basename(artifactPath)} matches ${basename(parsed.source)} — complete`,
  );
}

if (process.argv[1] && import.meta.url === new URL(`file://${process.argv[1]}`).href) {
  main();
}
