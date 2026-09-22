#!/usr/bin/env node

import { access, mkdir, readFile, readdir, rm, stat, writeFile } from 'node:fs/promises';
import { constants } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

import { hashArtifactTree } from './bundle-agent.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const desktopRoot = resolve(here, '..');

export const STAGING_DIR = join(desktopRoot, 'resources-mcp-servers');

const MISSING_MARKER = 'MCP-SERVERS-NOT-BUNDLED.txt';
export const MANIFEST = 'MCP-SERVERS-MANIFEST.txt';

export const SOURCE_ENV = 'GOOSAR_MCP_SERVERS_DIR';

const MCP_SDK_BREAKING_MAJOR = 2;

async function exists(path) {
  try {
    await access(path, constants.F_OK);
    return true;
  } catch {
    return false;
  }
}

function copyTree(src, dest) {
  const flags = process.platform === 'darwin' ? ['-Rc'] : ['-R'];
  execFileSync('cp', [...flags, src, dest], { stdio: 'inherit' });
}

export function mcpMajorVersion(version) {
  const match = /^\s*(?:\d+!)?(\d+)/.exec(version ?? '');
  return match ? Number.parseInt(match[1], 10) : null;
}

async function findSitePackages(venvDir) {
  const out = [];
  const libCandidates = [join(venvDir, 'lib'), join(venvDir, 'Lib')];
  for (const lib of libCandidates) {
    if (!(await exists(lib))) continue;
    const direct = join(lib, 'site-packages');
    if (await exists(direct)) out.push(direct);
    let entries;
    try {
      entries = await readdir(lib, { withFileTypes: true });
    } catch {
      entries = [];
    }
    for (const entry of entries) {
      if (!entry.isDirectory() || !entry.name.startsWith('python')) continue;
      const sp = join(lib, entry.name, 'site-packages');
      if (await exists(sp)) out.push(sp);
    }
  }
  return out;
}

export async function detectStagedServerMcpVersion(serverDir) {
  for (const venvName of ['.venv', 'venv']) {
    const venvDir = join(serverDir, venvName);
    if (!(await exists(venvDir))) continue;
    for (const sitePackages of await findSitePackages(venvDir)) {
      let entries;
      try {
        entries = await readdir(sitePackages, { withFileTypes: true });
      } catch {
        continue;
      }
      for (const entry of entries) {
        if (!entry.isDirectory()) continue;
        const match = /^mcp-(\d[^-]*)\.dist-info$/.exec(entry.name);
        if (match) return match[1];
      }
      for (const entry of entries) {
        if (!entry.isDirectory()) continue;
        if (!/^mcp-.*\.dist-info$/.test(entry.name)) continue;
        try {
          const metadata = await readFile(join(sitePackages, entry.name, 'METADATA'), 'utf-8');
          const line = metadata.split('\n').find((l) => /^Version:\s*/i.test(l));
          if (line) return line.replace(/^Version:\s*/i, '').trim();
        } catch {
          // Unreadable METADATA: treat as undetectable, keep scanning.
        }
      }
    }
  }
  return null;
}

async function listServerDirs(dir) {
  const entries = await readdir(dir, { withFileTypes: true });
  const names = [];
  const skipped = [];
  for (const entry of entries) {
    if (entry.name.startsWith('.')) continue;
    const full = join(dir, entry.name);
    let stats;
    try {
      stats = await stat(full);
    } catch (err) {
      const reason = err?.code === 'ENOENT' ? 'broken symlink' : (err?.message ?? String(err));
      console.warn(`[bundle-mcp-servers] WARN: skipped ${entry.name}: ${reason}`);
      skipped.push({ name: entry.name, reason });
      continue;
    }
    if (!stats.isDirectory()) {
      console.warn(`[bundle-mcp-servers] WARN: skipped ${entry.name}: not a directory`);
      skipped.push({ name: entry.name, reason: 'not a directory' });
      continue;
    }
    names.push(entry.name);
  }
  return { names: names.sort(), skipped };
}

async function writeMissingMarker(reason) {
  await rm(STAGING_DIR, { recursive: true, force: true });
  await mkdir(STAGING_DIR, { recursive: true });
  await writeFile(
    join(STAGING_DIR, MISSING_MARKER),
    'No MCP servers were bundled into this build.\n\n' +
      `Reason: ${reason}\n\n` +
      `Point ${SOURCE_ENV} at a directory whose child directories are each a\n` +
      'self-contained MCP server (its own venv) to stage them. Corporate\n' +
      'servers are injected this way at build time from the internal\n' +
      'provisioning bundle and are never committed to this public repository.\n' +
      'The app reports configured-but-unstaged servers as unavailable.\n',
    'utf-8',
  );
}

async function writeManifest(servers, { source }) {
  const lines = [
    'MCP servers staged into this build.',
    '',
    `source=${source}`,
    `count=${servers.length}`,
    `staged_at=${new Date().toISOString()}`,
    '',
    'Per server: name, sha256 (hashArtifactTree over the staged tree), mcp SDK',
    'version (or none when the server carries no Python `mcp` dependency).',
    '',
  ];
  for (const server of servers) {
    lines.push(`server=${server.name} sha256=${server.sha256} mcp=${server.mcpVersion ?? 'none'}`);
  }
  lines.push('');
  await writeFile(join(STAGING_DIR, MANIFEST), lines.join('\n'), 'utf-8');
}

export async function stageServers(sourceDir, { requireNoSkips = false } = {}) {
  await rm(STAGING_DIR, { recursive: true, force: true });
  await mkdir(STAGING_DIR, { recursive: true });

  const { names, skipped } = await listServerDirs(sourceDir);
  if (requireNoSkips && skipped.length > 0) {
    const detail = skipped.map((s) => `${s.name} (${s.reason})`).join(', ');
    throw new Error(
      `--require-mcp-servers was passed but ${skipped.length} source ` +
        `entr${skipped.length === 1 ? 'y was' : 'ies were'} skipped: ${detail}`,
    );
  }
  const staged = [];
  const pinViolations = [];

  for (const name of names) {
    const from = join(sourceDir, name);
    const to = join(STAGING_DIR, name);
    console.log(`[bundle-mcp-servers] staging ${name}\n  from ${from}\n  to   ${to}`);
    copyTree(from, to);

    const mcpVersion = await detectStagedServerMcpVersion(to);
    const major = mcpMajorVersion(mcpVersion);
    if (major !== null && major >= MCP_SDK_BREAKING_MAJOR) {
      pinViolations.push({ name, mcpVersion });
    }
    const sha256 = await hashArtifactTree(to);
    staged.push({ name, sha256, mcpVersion });
  }

  if (pinViolations.length > 0) {
    await rm(STAGING_DIR, { recursive: true, force: true });
    const detail = pinViolations.map((v) => `${v.name} (mcp ${v.mcpVersion})`).join(', ');
    throw new Error(
      `MCP SDK pin violation: ${detail} ship mcp >= ${MCP_SDK_BREAKING_MAJOR}, ` +
        'which removed mcp.server.fastmcp and renamed Tool.inputSchema and so ' +
        "breaks tool discovery for 1.x servers. Pin `mcp<2` in the server's " +
        'venv and re-stage.',
    );
  }

  await writeManifest(staged, { source: sourceDir });
  return staged;
}

async function main() {
  const argv = process.argv.slice(2);
  const requireServers = argv.includes('--require-mcp-servers');
  const sourceDir = process.env[SOURCE_ENV]?.trim();

  if (!sourceDir) {
    if (requireServers) {
      console.error(
        `[bundle-mcp-servers] ${SOURCE_ENV} is unset but --require-mcp-servers ` +
          'was passed — refusing to build without the MCP servers this release ' +
          'is expected to carry.',
      );
      process.exit(1);
    }
    console.warn(`[bundle-mcp-servers] ${SOURCE_ENV} is unset — building without MCP servers.`);
    await writeMissingMarker(`${SOURCE_ENV} is unset`);
    process.exit(0);
  }

  const resolved = resolve(sourceDir);
  if (!(await exists(resolved))) {
    console.error(
      `[bundle-mcp-servers] ${SOURCE_ENV}=${resolved} does not exist. An ` +
        'explicit source directory that is not present is a build mistake; ' +
        `unset ${SOURCE_ENV} to build without MCP servers on purpose.`,
    );
    process.exit(1);
  }

  let servers;
  try {
    servers = await stageServers(resolved, { requireNoSkips: requireServers });
  } catch (err) {
    console.error(`[bundle-mcp-servers] ${err instanceof Error ? err.message : err}`);
    process.exit(1);
    return;
  }

  if (servers.length === 0) {
    console.warn(
      `[bundle-mcp-servers] ${resolved} holds no server directories — staged 0 servers.`,
    );
  } else {
    console.log(
      `[bundle-mcp-servers] staged ${servers.length} server(s): ${servers
        .map((s) => `${s.name} (mcp ${s.mcpVersion ?? 'none'})`)
        .join(', ')}`,
    );
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  await main();
}
