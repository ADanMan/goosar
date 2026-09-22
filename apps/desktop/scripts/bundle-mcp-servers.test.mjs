import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { execFileSync, spawnSync } from 'node:child_process';
import {
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
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { detectStagedServerMcpVersion, mcpMajorVersion } from './bundle-mcp-servers.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const script = join(here, 'bundle-mcp-servers.mjs');
const desktopRoot = resolve(here, '..');
const stagingDir = join(desktopRoot, 'resources-mcp-servers');

let work;

function makeServer(name, { mcpVersion } = {}) {
  const dir = join(work, 'src', name);
  const sitePackages = join(dir, '.venv', 'lib', 'python3.11', 'site-packages');
  mkdirSync(sitePackages, { recursive: true });
  mkdirSync(join(dir, '.venv', 'bin'), { recursive: true });
  writeFileSync(
    join(dir, 'mcp-server.json'),
    JSON.stringify({ command: '.venv/bin/server', args: [] }),
  );
  writeFileSync(join(dir, '.venv', 'bin', 'server'), '#!/bin/sh\n');
  if (mcpVersion) {
    const distInfo = join(sitePackages, `mcp-${mcpVersion}.dist-info`);
    mkdirSync(distInfo, { recursive: true });
    writeFileSync(
      join(distInfo, 'METADATA'),
      `Metadata-Version: 2.1\nName: mcp\nVersion: ${mcpVersion}\n`,
    );
  }
  execFileSync('ln', ['-s', '../../../python/bin/python3.11', join(dir, '.venv', 'bin', 'python')]);
  return dir;
}

function readManifest() {
  const text = readFileSync(join(stagingDir, 'MCP-SERVERS-MANIFEST.txt'), 'utf-8');
  const servers = {};
  let count = null;
  for (const line of text.split('\n')) {
    const countMatch = line.match(/^count=(\d+)$/);
    if (countMatch) count = Number.parseInt(countMatch[1], 10);
    const serverMatch = line.match(/^server=(\S+) sha256=([0-9a-f]{64}) mcp=(\S+)$/);
    if (serverMatch) {
      servers[serverMatch[1]] = { sha256: serverMatch[2], mcp: serverMatch[3] };
    }
  }
  return { count, servers };
}

function runScript(env = {}, args = []) {
  const result = spawnSync('node', [script, ...args], {
    cwd: desktopRoot,
    encoding: 'utf-8',
    env: { ...process.env, ...env },
  });
  return {
    status: result.status,
    output: `${result.stdout ?? ''}${result.stderr ?? ''}`,
  };
}

beforeEach(() => {
  work = mkdtempSync(join(tmpdir(), 'bundle-mcp-'));
  mkdirSync(join(work, 'src'), { recursive: true });
  rmSync(stagingDir, { recursive: true, force: true });
});

afterEach(() => {
  rmSync(work, { recursive: true, force: true });
  rmSync(stagingDir, { recursive: true, force: true });
});

describe('mcpMajorVersion', () => {
  it('reads the leading integer of a PEP 440 version', () => {
    expect(mcpMajorVersion('1.9.0')).toBe(1);
    expect(mcpMajorVersion('2.0.0')).toBe(2);
    expect(mcpMajorVersion('2.0.0rc1')).toBe(2);
    expect(mcpMajorVersion('10.1.2')).toBe(10);
  });

  it('returns null for a non-numeric or empty version', () => {
    expect(mcpMajorVersion(null)).toBeNull();
    expect(mcpMajorVersion('')).toBeNull();
    expect(mcpMajorVersion('unknown')).toBeNull();
  });
});

describe('detectStagedServerMcpVersion', () => {
  it('reads the version from the dist-info directory name', async () => {
    const dir = makeServer('good', { mcpVersion: '1.9.0' });
    await expect(detectStagedServerMcpVersion(dir)).resolves.toBe('1.9.0');
  });

  it('returns null for a server with no mcp package', async () => {
    const dir = makeServer('node-server', {});
    await expect(detectStagedServerMcpVersion(dir)).resolves.toBeNull();
  });
});

describe('bundle-mcp-servers CLI', () => {
  it('stages nothing and writes a marker when the source env is unset', () => {
    const { status, output } = runScript({ GOOSAR_MCP_SERVERS_DIR: '' });
    expect(status).toBe(0);
    expect(output).toMatch(/building without MCP servers/);
    expect(existsSync(join(stagingDir, 'MCP-SERVERS-NOT-BUNDLED.txt'))).toBe(true);
  });

  it('hard-fails when the source env points at a non-existent directory', () => {
    const { status, output } = runScript({
      GOOSAR_MCP_SERVERS_DIR: join(work, 'does-not-exist'),
    });
    expect(status).toBe(1);
    expect(output).toMatch(/does not exist/);
  });

  it('hard-fails when unset but --require-mcp-servers is passed', () => {
    const { status, output } = runScript({ GOOSAR_MCP_SERVERS_DIR: '' }, ['--require-mcp-servers']);
    expect(status).toBe(1);
    expect(output).toMatch(/refusing to build without the MCP servers/);
  });

  it('stages a well-pinned server, preserving its symlinks, and writes a manifest', () => {
    makeServer('outlook', { mcpVersion: '1.9.0' });

    const { status, output } = runScript({
      GOOSAR_MCP_SERVERS_DIR: join(work, 'src'),
    });

    expect(status).toBe(0);
    expect(output).toMatch(/staged 1 server\(s\): outlook \(mcp 1\.9\.0\)/);
    expect(existsSync(join(stagingDir, 'outlook', 'mcp-server.json'))).toBe(true);
    const link = join(stagingDir, 'outlook', '.venv', 'bin', 'python');
    expect(lstatSync(link).isSymbolicLink()).toBe(true);
    expect(readlinkSync(link)).toBe('../../../python/bin/python3.11');
    const manifest = readManifest();
    expect(manifest.count).toBe(1);
    expect(manifest.servers.outlook.mcp).toBe('1.9.0');
    expect(manifest.servers.outlook.sha256).toMatch(/^[0-9a-f]{64}$/);
  });

  it('stages several servers and lists each in the manifest', () => {
    makeServer('outlook', { mcpVersion: '1.9.0' });
    makeServer('fetch', { mcpVersion: '1.12.3' });

    const { status } = runScript({ GOOSAR_MCP_SERVERS_DIR: join(work, 'src') });

    expect(status).toBe(0);
    const manifest = readManifest();
    expect(manifest.count).toBe(2);
    expect(Object.keys(manifest.servers).sort()).toEqual(['fetch', 'outlook']);
  });

  it('FAILS staging when any server ships mcp >= 2 and leaves nothing behind', () => {
    makeServer('outlook', { mcpVersion: '1.9.0' });
    makeServer('broken', { mcpVersion: '2.0.0' });

    const { status, output } = runScript({
      GOOSAR_MCP_SERVERS_DIR: join(work, 'src'),
    });

    expect(status).toBe(1);
    expect(output).toMatch(/MCP SDK pin violation/);
    expect(output).toMatch(/broken \(mcp 2\.0\.0\)/);
    expect(existsSync(join(stagingDir, 'broken'))).toBe(false);
    expect(existsSync(join(stagingDir, 'outlook'))).toBe(false);
    expect(existsSync(join(stagingDir, 'MCP-SERVERS-MANIFEST.txt'))).toBe(false);
  });

  it('stages zero servers for an existing-but-empty source dir', () => {
    const { status } = runScript({ GOOSAR_MCP_SERVERS_DIR: join(work, 'src') });
    expect(status).toBe(0);
    const manifest = readManifest();
    expect(manifest.count).toBe(0);
  });

  it('stages a server that is a symlink to a directory elsewhere (issue #696)', () => {
    const real = makeServer('ews-mcp', { mcpVersion: '1.9.0' });
    rmSync(real, { recursive: true, force: true });
    const target = join(work, 'elsewhere', 'ews-mcp');
    mkdirSync(dirname(target), { recursive: true });
    const rebuilt = makeServer('ews-mcp', { mcpVersion: '1.9.0' });
    rmSync(target, { recursive: true, force: true });
    execFileSync('mv', [rebuilt, target]);
    execFileSync('ln', ['-s', target, join(work, 'src', 'ews-mcp')]);

    const { status, output } = runScript({
      GOOSAR_MCP_SERVERS_DIR: join(work, 'src'),
    });

    expect(status).toBe(0);
    expect(output).toMatch(/staged 1 server\(s\): ews-mcp/);
    expect(existsSync(join(stagingDir, 'ews-mcp', 'mcp-server.json'))).toBe(true);
  });

  it('skips a plain file with a WARN naming the reason, and keeps building', () => {
    makeServer('outlook', { mcpVersion: '1.9.0' });
    writeFileSync(join(work, 'src', 'README.txt'), 'not a server\n');

    const { status, output } = runScript({
      GOOSAR_MCP_SERVERS_DIR: join(work, 'src'),
    });

    expect(status).toBe(0);
    expect(output).toMatch(/WARN: skipped README\.txt: not a directory/);
    expect(output).toMatch(/staged 1 server\(s\): outlook/);
  });

  it('skips a broken symlink with a WARN and does not crash', () => {
    makeServer('outlook', { mcpVersion: '1.9.0' });
    execFileSync('ln', ['-s', join(work, 'src', 'does-not-exist'), join(work, 'src', 'ghost')]);

    const { status, output } = runScript({
      GOOSAR_MCP_SERVERS_DIR: join(work, 'src'),
    });

    expect(status).toBe(0);
    expect(output).toMatch(/WARN: skipped ghost: broken symlink/);
    expect(output).toMatch(/staged 1 server\(s\): outlook/);
  });

  it('ignores a hidden directory silently (no WARN)', () => {
    makeServer('outlook', { mcpVersion: '1.9.0' });
    mkdirSync(join(work, 'src', '.DS_Store_dir'));

    const { status, output } = runScript({
      GOOSAR_MCP_SERVERS_DIR: join(work, 'src'),
    });

    expect(status).toBe(0);
    expect(output).not.toMatch(/DS_Store_dir/);
  });

  it('FAILS with --require-mcp-servers when any source entry was skipped', () => {
    makeServer('outlook', { mcpVersion: '1.9.0' });
    writeFileSync(join(work, 'src', 'README.txt'), 'not a server\n');

    const { status, output } = runScript({ GOOSAR_MCP_SERVERS_DIR: join(work, 'src') }, [
      '--require-mcp-servers',
    ]);

    expect(status).toBe(1);
    expect(output).toMatch(/README\.txt/);
    expect(existsSync(join(stagingDir, 'outlook'))).toBe(false);
  });
});
