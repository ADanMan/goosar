import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { execFileSync } from 'node:child_process';
import {
  appendFileSync,
  existsSync,
  lstatSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readlinkSync,
  rmSync,
  writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { hashArtifactTree } from '../../scripts/hash-artifact-tree.mjs';
import type { AgentPathContext } from './agent-bootstrap';
import {
  appendStagedMcpServerPath,
  ensureBundledMcpServers,
  firstConfiguredMcpServer,
  mcpServersRoot,
  readMcpServersStatus,
  stagedMcpServerBinDirs,
} from './mcp-servers';

let home: string;
let bundled: string;

function ctx(overrides: Partial<AgentPathContext> = {}): AgentPathContext {
  return { home, env: { HOME: home }, ...overrides };
}

async function makeBundledServer(name: string, marker: string): Promise<void> {
  const dir = join(bundled, name);
  mkdirSync(join(dir, '.venv', 'bin'), { recursive: true });
  writeFileSync(join(dir, 'server.py'), marker);
  writeFileSync(join(dir, '.venv', 'bin', 'server'), '#!/bin/sh\n');
  execFileSync('ln', ['-s', '../../../python/bin/python3.11', join(dir, '.venv', 'bin', 'python')]);
  const sha256 = await hashArtifactTree(dir);
  appendFileSync(
    join(bundled, 'MCP-SERVERS-MANIFEST.txt'),
    `server=${name} sha256=${sha256} mcp=none\n`,
  );
}

beforeEach(() => {
  home = mkdtempSync(join(tmpdir(), 'goosar-mcp-home-'));
  bundled = mkdtempSync(join(tmpdir(), 'goosar-mcp-bundled-'));
});

afterEach(() => {
  rmSync(home, { recursive: true, force: true });
  rmSync(bundled, { recursive: true, force: true });
});

describe('readMcpServersStatus', () => {
  it('reports no servers on a fresh machine', async () => {
    await expect(readMcpServersStatus(ctx())).resolves.toEqual({ installed: [] });
  });
});

describe('ensureBundledMcpServers', () => {
  it('does nothing when this build carries no bundled servers', async () => {
    const status = await ensureBundledMcpServers(ctx(), null);
    expect(status).toEqual({
      installed: [],
      staged: [],
      skippedExisting: [],
      refused: [],
    });
  });

  it('stages an absent server, preserving its symlinks', async () => {
    await makeBundledServer('outlook', 'v1');

    const status = await ensureBundledMcpServers(ctx(), bundled);

    expect(status.staged).toEqual(['outlook']);
    expect(status.skippedExisting).toEqual([]);
    const target = join(mcpServersRoot(ctx()), 'outlook');
    expect(readFileSync(join(target, 'server.py'), 'utf-8')).toBe('v1');
    const link = join(target, '.venv', 'bin', 'python');
    expect(lstatSync(link).isSymbolicLink()).toBe(true);
    expect(readlinkSync(link)).toBe('../../../python/bin/python3.11');
  });

  it('never overwrites a server the user already has', async () => {
    const target = join(mcpServersRoot(ctx()), 'outlook');
    mkdirSync(target, { recursive: true });
    writeFileSync(join(target, 'server.py'), 'user-edited');

    await makeBundledServer('outlook', 'shipped');
    const status = await ensureBundledMcpServers(ctx(), bundled);

    expect(status.staged).toEqual([]);
    expect(status.skippedExisting).toEqual(['outlook']);
    expect(readFileSync(join(target, 'server.py'), 'utf-8')).toBe('user-edited');
  });

  it('stages only the absent servers when some already exist', async () => {
    const existing = join(mcpServersRoot(ctx()), 'outlook');
    mkdirSync(existing, { recursive: true });
    writeFileSync(join(existing, 'server.py'), 'user-edited');

    await makeBundledServer('outlook', 'shipped');
    await makeBundledServer('fetch', 'shipped');

    const status = await ensureBundledMcpServers(ctx(), bundled);

    expect(status.staged).toEqual(['fetch']);
    expect(status.skippedExisting).toEqual(['outlook']);
    expect(status.installed.sort()).toEqual(['fetch', 'outlook']);
  });

  it('is idempotent — a second run stages nothing new', async () => {
    await makeBundledServer('outlook', 'v1');
    await ensureBundledMcpServers(ctx(), bundled);
    const second = await ensureBundledMcpServers(ctx(), bundled);
    expect(second.staged).toEqual([]);
    expect(second.skippedExisting).toEqual(['outlook']);
  });

  it('refuses a server whose tree no longer matches the manifest, but stages the good ones', async () => {
    await makeBundledServer('outlook', 'v1');
    await makeBundledServer('fetch', 'v1');
    writeFileSync(join(bundled, 'outlook', 'server.py'), 'TAMPERED');

    const status = await ensureBundledMcpServers(ctx(), bundled);

    expect(status.refused).toEqual(['outlook']);
    expect(status.staged).toEqual(['fetch']);
    expect(status.installed).toEqual(['fetch']);
    expect(existsSync(join(mcpServersRoot(ctx()), 'outlook'))).toBe(false);
  });

  it('refuses a server that the manifest does not cover', async () => {
    await makeBundledServer('outlook', 'v1'); 
    mkdirSync(join(bundled, 'rogue'), { recursive: true });
    writeFileSync(join(bundled, 'rogue', 'server.py'), 'who-am-i');

    const status = await ensureBundledMcpServers(ctx(), bundled);

    expect(status.staged).toEqual(['outlook']);
    expect(status.refused).toEqual(['rogue']);
    expect(existsSync(join(mcpServersRoot(ctx()), 'rogue'))).toBe(false);
  });
});

function makeStagedServer(name: string, binary: string): string {
  const binDir = join(mcpServersRoot(ctx()), name, '.venv', 'bin');
  mkdirSync(binDir, { recursive: true });
  writeFileSync(join(binDir, binary), '#!/bin/sh\n', { mode: 0o755 });
  return binDir;
}

describe('stagedMcpServerBinDirs', () => {
  it('returns [] when no servers are staged', () => {
    expect(stagedMcpServerBinDirs(ctx(), 'linux')).toEqual([]);
  });

  it('lists the .venv/bin of each staged server', () => {
    const outlook = makeStagedServer('outlook', 'ewsmcp');
    const atlassian = makeStagedServer('atlassian', 'mcp-atlassian');
    expect(stagedMcpServerBinDirs(ctx(), 'linux').sort()).toEqual([outlook, atlassian].sort());
  });
});

describe('appendStagedMcpServerPath', () => {
  it('returns the base PATH unchanged when nothing is staged', () => {
    expect(appendStagedMcpServerPath('/usr/bin:/bin', ctx(), 'linux')).toBe('/usr/bin:/bin');
  });

  it("makes a bare staged command resolvable, appended AFTER the caller's PATH", () => {
    const staged = makeStagedServer('outlook', 'ewsmcp');
    const result = appendStagedMcpServerPath('/usr/bin:/bin', ctx(), 'linux');
    const parts = result.split(':');
    expect(parts).toContain(staged);
    expect(existsSync(join(staged, 'ewsmcp'))).toBe(true);
    expect(parts.indexOf(staged)).toBeGreaterThan(parts.indexOf('/usr/bin'));
    expect(parts.indexOf(staged)).toBeGreaterThan(parts.indexOf('/bin'));
  });

  it('keeps an operator-installed server (real PATH) winning over the staged copy', () => {
    const realDir = mkdtempSync(join(tmpdir(), 'goosar-real-path-'));
    writeFileSync(join(realDir, 'ewsmcp'), '#!/bin/sh\n', { mode: 0o755 });
    const staged = makeStagedServer('outlook', 'ewsmcp');

    const parts = appendStagedMcpServerPath(realDir, ctx(), 'linux').split(':');
    expect(parts.indexOf(realDir)).toBeLessThan(parts.indexOf(staged));

    rmSync(realDir, { recursive: true, force: true });
  });

  it('returns just the staged dirs when the caller PATH is empty', () => {
    const staged = makeStagedServer('outlook', 'ewsmcp');
    expect(appendStagedMcpServerPath('', ctx(), 'linux')).toBe(staged);
    expect(appendStagedMcpServerPath(undefined, ctx(), 'linux')).toBe(staged);
  });
});

describe('firstConfiguredMcpServer', () => {
  it('returns null when no server is staged', async () => {
    expect(await firstConfiguredMcpServer(ctx())).toBeNull();
  });

  it('returns the alphabetically first staged server', async () => {
    mkdirSync(join(mcpServersRoot(ctx()), 'zeta-mcp'), { recursive: true });
    mkdirSync(join(mcpServersRoot(ctx()), 'alpha-mcp'), { recursive: true });

    const first = await firstConfiguredMcpServer(ctx());
    expect(first?.name).toBe('alpha-mcp');
    expect(first?.dir).toBe(join(mcpServersRoot(ctx()), 'alpha-mcp'));
  });
});
