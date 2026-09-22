import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { execFileSync } from 'node:child_process';
import {
  existsSync,
  lstatSync,
  mkdirSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  readlinkSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { zstdCompressSync } from 'node:zlib';

vi.mock('electron', () => ({
  ipcMain: { handle: vi.fn() },
  app: { isPackaged: false, getAppPath: () => '', getPath: () => '' },
  session: { defaultSession: { setProxy: vi.fn(), fetch: vi.fn() } },
  powerMonitor: { on: vi.fn() },
}));

import { hashArtifactTree } from '../../scripts/hash-artifact-tree.mjs';
import type { AgentPathContext } from './agent-bootstrap';
import {
  atomicSwapInstall,
  bindProvisioningCredentials,
  bindProvisioningWorkspace,
  listInstalledProvisionedPackages,
  packageFamilyRoot,
  parseRevokedPackageRef,
  parseRevokedPackages,
  removeOneRevokedPackage,
  extractPackageTar,
  getProvisioningStatus,
  defaultZstdDecompress,
  nodeZstdDecompress,
  packageInstallDir,
  packageNeedsSync,
  parseManifestResponse,
  parsePackageManifest,
  probeProvisioningReachability,
  readProvisionedMarker,
  readTarEntries,
  resetProvisioningStatusForTests,
  retryProvisioning,
  readBakedStoreManifest,
  resolveProvisionedRuntimeDir,
  runtimePackageRoot,
  setProvisioningCredentials,
  setProvisioningRestartHook,
  setProvisioningRestartPending,
  setProvisioningWorkspaceId,
  sha256Hex,
  syncOnePackage,
  runWorkspaceAccessRevokedCleanup,
  syncProvisionedPackages,
  systemZstdDecompress,
  validateTarEntries,
  type AuthedFetch,
  type PackageManifest,
  type ProvisionedMarker,
  parseManifestCoverage,
  currentPackagePlatform,
  parseUnavailablePackages,
  unavailablePackageName,
} from './provisioning';

let home: string;

function ctx(): AgentPathContext {
  return { home, env: { HOME: home } };
}

beforeEach(() => {
  home = mkdtempSync(join(tmpdir(), 'goosar-provisioning-home-'));
  setProvisioningCredentials(null, null);
  setProvisioningWorkspaceId(null);
  resetProvisioningStatusForTests();
  setProvisioningRestartHook(null);
});

afterEach(() => {
  rmSync(home, { recursive: true, force: true });
});

function cstrField(value: string, length: number): Buffer {
  const buf = Buffer.alloc(length);
  buf.write(value, 0, 'utf-8');
  return buf;
}

function octalField(value: number, length: number): Buffer {
  const buf = Buffer.alloc(length);
  buf.write(value.toString(8).padStart(length - 1, '0'), 0, 'ascii');
  return buf;
}

interface FixtureEntry {
  name: string;
  type: 'file' | 'symlink' | 'directory';
  data?: Buffer;
  linkname?: string;
  mode?: number;
}

function buildTarHeader(entry: FixtureEntry, size: number): Buffer {
  const header = Buffer.alloc(512);
  cstrField(entry.name, 100).copy(header, 0);
  octalField(entry.mode ?? 0o644, 8).copy(header, 100);
  cstrField('0000000', 8).copy(header, 108);
  cstrField('0000000', 8).copy(header, 116);
  octalField(size, 12).copy(header, 124);
  octalField(0, 12).copy(header, 136);
  cstrField('        ', 8).copy(header, 148);
  const typeflag = { file: '0', symlink: '2', directory: '5' }[entry.type];
  header[156] = typeflag.charCodeAt(0);
  cstrField(entry.linkname ?? '', 100).copy(header, 157);
  return header;
}

function buildTar(entries: FixtureEntry[]): Buffer {
  const chunks: Buffer[] = [];
  for (const entry of entries) {
    const data = entry.data ?? Buffer.alloc(0);
    chunks.push(buildTarHeader(entry, data.length));
    if (entry.type === 'file') {
      chunks.push(data);
      const pad = (512 - (data.length % 512)) % 512;
      if (pad > 0) chunks.push(Buffer.alloc(pad));
    }
  }
  chunks.push(Buffer.alloc(1024)); 
  return Buffer.concat(chunks);
}

function buildMetaHeaderBlock(typeflag: 'x' | 'g' | 'L' | 'K', dataStr: string): Buffer {
  const data = Buffer.from(dataStr, 'utf-8');
  const header = Buffer.alloc(512);
  const name = typeflag === 'L' || typeflag === 'K' ? '././@LongLink' : 'PaxHeaders.0/x';
  cstrField(name, 100).copy(header, 0);
  octalField(data.length, 12).copy(header, 124);
  header[156] = typeflag.charCodeAt(0);
  const pad = (512 - (data.length % 512)) % 512;
  return Buffer.concat([header, data, Buffer.alloc(pad)]);
}

function paxRecord(key: string, value: string): string {
  let len = key.length + value.length + 3; 
  for (;;) {
    const candidate = String(len).length + key.length + value.length + 3;
    if (candidate === len) break;
    len = candidate;
  }
  return `${len} ${key}=${value}\n`;
}

function base256Field(value: number, length: number): Buffer {
  const buf = Buffer.alloc(length);
  buf[0] = 0x80;
  let v = BigInt(value);
  for (let i = length - 1; i >= 1; i -= 1) {
    buf[i] = Number(v & 0xffn);
    v >>= 8n;
  }
  return buf;
}

describe('parsePackageManifest', () => {
  const valid = {
    schemaVersion: 1,
    name: 'office-docx',
    version: '1.4.0',
    type: 'skill',
    platform: 'darwin-arm64',
    sha256: 'a'.repeat(64),
    size: 123,
    requires: ['runtime:playwright-browsers@1.0.0'],
  };

  it('accepts a well-formed manifest entry', () => {
    expect(parsePackageManifest(valid)).toEqual({ ...valid, sha256: 'a'.repeat(64) });
  });

  it('rejects a malformed manifest response instead of trusting it (API compatibility)', () => {
    expect(parsePackageManifest(null)).toBeNull();
    expect(parsePackageManifest({ ...valid, schemaVersion: 2 })).toBeNull();
    expect(parsePackageManifest({ ...valid, type: 'not-a-type' })).toBeNull();
    expect(parsePackageManifest({ ...valid, platform: 'amiga-68k' })).toBeNull();
    expect(parsePackageManifest({ ...valid, sha256: 'not-hex' })).toBeNull();
    expect(parsePackageManifest({ ...valid, size: -1 })).toBeNull();
    expect(parsePackageManifest({ ...valid, requires: [1, 2] })).toBeNull();
  });

  it('drops only the malformed entry from a manifest response, keeping the rest', () => {
    const packages = parseManifestResponse({
      schemaVersion: 1,
      packages: [valid, { ...valid, name: '' }, { ...valid, name: 'good-two' }],
    });
    expect(packages.map((p) => p.name)).toEqual(['office-docx', 'good-two']);
  });

  it('returns an empty list for a response with no packages array', () => {
    expect(parseManifestResponse({})).toEqual([]);
    expect(parseManifestResponse(null)).toEqual([]);
  });

  it('defaults a MISSING requires field to [] instead of rejecting the entry', () => {
    const { requires, ...withoutRequires } = valid;
    expect(parsePackageManifest(withoutRequires)).toEqual({
      ...withoutRequires,
      sha256: 'a'.repeat(64),
      requires: [],
    });
  });

  it('still rejects a PRESENT but non-array requires field', () => {
    expect(parsePackageManifest({ ...valid, requires: 'not-an-array' })).toBeNull();
    expect(parsePackageManifest({ ...valid, requires: null })).toBeNull();
  });
});

describe('parseManifestCoverage', () => {
  it('reads the coverage fields and tolerates a server without them', () => {
    expect(
      parseManifestCoverage({
        packages: [],
        totalBeforePlatformFilter: 12,
        platformsAvailable: ['*', 'darwin-arm64'],
      }),
    ).toEqual({ totalBeforePlatformFilter: 12, platformsAvailable: ['*', 'darwin-arm64'] });
    expect(parseManifestCoverage({ packages: [] })).toEqual({
      totalBeforePlatformFilter: 0,
      platformsAvailable: [],
    });
    expect(
      parseManifestCoverage({
        totalBeforePlatformFilter: -3,
        platformsAvailable: [1, '', 'linux-x64'],
      }),
    ).toEqual({ totalBeforePlatformFilter: 0, platformsAvailable: ['linux-x64'] });
    expect(parseManifestCoverage(null)).toEqual({
      totalBeforePlatformFilter: 0,
      platformsAvailable: [],
    });
  });
});

describe('currentPackagePlatform', () => {
  it('maps every desktop build target', () => {
    expect(currentPackagePlatform('darwin', 'arm64')).toBe('darwin-arm64');
    expect(currentPackagePlatform('darwin', 'x64')).toBe('darwin-x64');
    expect(currentPackagePlatform('win32', 'x64')).toBe('win-x64');
    expect(currentPackagePlatform('linux', 'x64')).toBe('linux-x64');
    expect(currentPackagePlatform('linux', 'arm64')).toBe('linux-arm64');
  });
});

describe('packageNeedsSync', () => {
  const pkg = { version: '1.4.0', sha256: 'b'.repeat(64) };

  it('needs sync when nothing is installed', () => {
    expect(packageNeedsSync(pkg, null)).toBe(true);
  });

  it('needs sync when the installed version differs from the manifest', () => {
    const marker: ProvisionedMarker = {
      name: 'x',
      version: '1.3.0',
      platform: 'darwin-arm64',
      sha256: pkg.sha256,
      installedAt: '2024-01-01T00:00:00.000Z',
    };
    expect(packageNeedsSync(pkg, marker)).toBe(true);
  });

  it('needs sync when the digest changed even at an unchanged version string', () => {
    const marker: ProvisionedMarker = {
      name: 'x',
      version: pkg.version,
      platform: 'darwin-arm64',
      sha256: 'c'.repeat(64),
      installedAt: '2024-01-01T00:00:00.000Z',
    };
    expect(packageNeedsSync(pkg, marker)).toBe(true);
  });

  it('does NOT need sync when version and digest both match', () => {
    const marker: ProvisionedMarker = {
      name: 'x',
      version: pkg.version,
      platform: 'darwin-arm64',
      sha256: pkg.sha256,
      installedAt: '2024-01-01T00:00:00.000Z',
    };
    expect(packageNeedsSync(pkg, marker)).toBe(false);
  });
});

describe('readTarEntries + validateTarEntries', () => {
  it('rejects a `..` path-traversal entry', () => {
    const entries = readTarEntries(
      buildTar([{ name: '../escape.txt', type: 'file', data: Buffer.from('pwned') }]),
    );
    const violations = validateTarEntries(entries);
    expect(violations).toHaveLength(1);
    expect(violations[0].name).toBe('../escape.txt');
    expect(violations[0].reason).toMatch(/escapes the extraction root/);
  });

  it('rejects an absolute-path entry', () => {
    const entries = readTarEntries(
      buildTar([{ name: '/etc/passwd', type: 'file', data: Buffer.from('x') }]),
    );
    expect(validateTarEntries(entries)).toHaveLength(1);
  });

  it('rejects a symlink whose target escapes the extraction root via `..`', () => {
    const entries = readTarEntries(
      buildTar([{ name: 'bin/evil', type: 'symlink', linkname: '../../../etc/passwd' }]),
    );
    const violations = validateTarEntries(entries);
    expect(violations).toHaveLength(1);
    expect(violations[0].reason).toMatch(/symlink target escapes/);
  });

  it('rejects a symlink with an absolute escaping target', () => {
    const entries = readTarEntries(
      buildTar([{ name: 'bin/evil', type: 'symlink', linkname: '/etc/passwd' }]),
    );
    expect(validateTarEntries(entries)).toHaveLength(1);
  });

  it("preserves an internal relative symlink (a venv's bin -> ../lib layout)", () => {
    const entries = readTarEntries(
      buildTar([
        { name: 'lib/python3.11/real.py', type: 'file', data: Buffer.from('x') },
        {
          name: 'bin/python',
          type: 'symlink',
          linkname: '../lib/python3.11/real.py',
        },
      ]),
    );
    expect(validateTarEntries(entries)).toEqual([]);
  });

  it('rejects a Windows-style backslash path-traversal entry (`..\\..\\..\\evil.js`)', () => {
    const entries = readTarEntries(
      buildTar([{ name: '..\\..\\..\\evil.js', type: 'file', data: Buffer.from('pwned') }]),
    );
    const violations = validateTarEntries(entries);
    expect(violations).toHaveLength(1);
    expect(violations[0].reason).toMatch(/escapes the extraction root/);
  });

  it('rejects a backslash-rooted (drive-relative/UNC-style) entry name', () => {
    const entries = readTarEntries(
      buildTar([{ name: '\\Windows\\System32\\evil.dll', type: 'file', data: Buffer.from('x') }]),
    );
    expect(validateTarEntries(entries)).toHaveLength(1);
  });

  it('rejects a symlink whose target escapes via backslash-separated `..` segments', () => {
    const entries = readTarEntries(
      buildTar([{ name: 'bin/evil', type: 'symlink', linkname: '..\\..\\..\\Windows\\System32' }]),
    );
    const violations = validateTarEntries(entries);
    expect(violations).toHaveLength(1);
    expect(violations[0].reason).toMatch(/symlink target escapes/);
  });

  it('rejects a symlink with a UNC-style backslash-rooted target', () => {
    const entries = readTarEntries(
      buildTar([
        { name: 'bin/evil', type: 'symlink', linkname: '\\\\attacker-host\\share\\payload' },
      ]),
    );
    expect(validateTarEntries(entries)).toHaveLength(1);
  });
});

describe('readTarEntries: PAX / GNU long-name / long-link / base-256 support', () => {
  it("does not desync the scan on a PAX extended header ('x') with nonzero size, and applies its path= override", () => {
    const longName = 'site-packages/' + 'dependency-name-segment/'.repeat(6) + 'module.py';
    const paxBlock = buildMetaHeaderBlock('x', paxRecord('path', longName));
    const fileBlock = buildTar([
      { name: 'placeholder-name-gets-overridden', type: 'file', data: Buffer.from('payload') },
    ]);

    const entries = readTarEntries(Buffer.concat([paxBlock, fileBlock]));

    expect(entries).toHaveLength(1);
    expect(entries[0].name).toBe(longName);
    expect(entries[0].data.toString('utf-8')).toBe('payload');
  });

  it('applies both path= and linkpath= PAX overrides from a single extended header to a symlink entry', () => {
    const longName = 'a/'.repeat(60) + 'link';
    const longTarget = '../' + 'b/'.repeat(40) + 'real-file';
    const paxData = paxRecord('path', longName) + paxRecord('linkpath', longTarget);
    const paxBlock = buildMetaHeaderBlock('x', paxData);
    const symlinkBlock = buildTar([
      { name: 'placeholder', type: 'symlink', linkname: 'placeholder-target' },
    ]);

    const entries = readTarEntries(Buffer.concat([paxBlock, symlinkBlock]));

    expect(entries).toHaveLength(1);
    expect(entries[0].name).toBe(longName);
    expect(entries[0].linkname).toBe(longTarget);
  });

  it("does not desync the scan on a GNU long-name ('L') entry, and applies the override", () => {
    const longName = 'b'.repeat(150);
    const lBlock = buildMetaHeaderBlock('L', `${longName}\0`);
    const fileBlock = buildTar([{ name: 'short', type: 'file', data: Buffer.from('v') }]);

    const entries = readTarEntries(Buffer.concat([lBlock, fileBlock]));

    expect(entries).toHaveLength(1);
    expect(entries[0].name).toBe(longName);
    expect(entries[0].data.toString('utf-8')).toBe('v');
  });

  it("does not desync the scan on a GNU long-link ('K') entry, and applies the override", () => {
    const longTarget = '../' + 'c'.repeat(150);
    const kBlock = buildMetaHeaderBlock('K', `${longTarget}\0`);
    const symlinkBlock = buildTar([
      { name: 'bin/tool', type: 'symlink', linkname: 'placeholder-target' },
    ]);

    const entries = readTarEntries(Buffer.concat([kBlock, symlinkBlock]));

    expect(entries).toHaveLength(1);
    expect(entries[0].linkname).toBe(longTarget);
  });

  it('parses a GNU base-256 encoded size field correctly (rather than returning NaN)', () => {
    const payload = Buffer.from('hello-base256-size-field');
    const header = Buffer.alloc(512);
    cstrField('base256.txt', 100).copy(header, 0);
    base256Field(payload.length, 12).copy(header, 124);
    header[156] = '0'.charCodeAt(0);
    const pad = (512 - (payload.length % 512)) % 512;
    const buf = Buffer.concat([header, payload, Buffer.alloc(pad), Buffer.alloc(1024)]);

    const entries = readTarEntries(buf);

    expect(entries).toHaveLength(1);
    expect(entries[0].data.toString('utf-8')).toBe(payload.toString('utf-8'));
  });

  it('fails closed (throws) on an unparseable, non-octal, non-base-256 size field instead of silently returning NaN and desyncing the scan', () => {
    const header = Buffer.alloc(512);
    cstrField('garbage.txt', 100).copy(header, 0);
    cstrField('not-octal!!', 12).copy(header, 124);
    header[156] = '0'.charCodeAt(0);
    const buf = Buffer.concat([header, Buffer.alloc(1024)]);

    expect(() => readTarEntries(buf)).toThrow(/invalid octal tar header field/);
  });
});

describe('extractPackageTar', () => {
  it('refuses to extract ANYTHING when the archive contains a `..` entry or an escaping symlink', async () => {
    const dest = join(home, 'staging-ziplip');
    const tar = buildTar([
      { name: 'good.txt', type: 'file', data: Buffer.from('ok') },
      { name: '../escape.txt', type: 'file', data: Buffer.from('pwned') },
      { name: 'evil-link', type: 'symlink', linkname: '/etc/passwd' },
    ]);

    const result = await extractPackageTar(tar, dest);

    expect(result.ok).toBe(false);
    expect(existsSync(join(dest, 'good.txt'))).toBe(false);
  });

  it('extracts a safe archive and preserves the internal relative symlink, and the tree hash gate passes', async () => {
    const dest = join(home, 'staging-safe');
    const tar = buildTar([
      { name: 'lib/python3.11/real.py', type: 'file', data: Buffer.from('print(1)') },
      {
        name: 'bin/python',
        type: 'symlink',
        linkname: '../lib/python3.11/real.py',
      },
    ]);

    const result = await extractPackageTar(tar, dest);

    expect(result.ok).toBe(true);
    expect(readFileSync(join(dest, 'lib', 'python3.11', 'real.py'), 'utf-8')).toBe('print(1)');
    const link = join(dest, 'bin', 'python');
    expect(lstatSync(link).isSymbolicLink()).toBe(true);
    expect(readlinkSync(link)).toBe('../lib/python3.11/real.py');

    const first = await hashArtifactTree(dest);
    const second = await hashArtifactTree(dest);
    expect(first).toBe(second);
    expect(first).toMatch(/^[0-9a-f]{64}$/);
  });

  it('fails closed (does not throw) when the tar has an unparseable header field, and writes nothing', async () => {
    const dest = join(home, 'staging-garbage-size');
    const header = Buffer.alloc(512);
    cstrField('garbage.txt', 100).copy(header, 0);
    cstrField('not-octal!!', 12).copy(header, 124);
    header[156] = '0'.charCodeAt(0);
    const buf = Buffer.concat([header, Buffer.alloc(1024)]);

    const result = await extractPackageTar(buf, dest);

    expect(result.ok).toBe(false);
    expect(existsSync(dest)).toBe(false);
  });

  it('preserves the executable bit for a 0755 file and leaves a 0644 file non-executable', async () => {
    const dest = join(home, 'staging-mode');
    const tar = buildTar([
      { name: 'bin/run.sh', type: 'file', data: Buffer.from('#!/bin/sh\n'), mode: 0o755 },
      { name: 'README.md', type: 'file', data: Buffer.from('docs'), mode: 0o644 },
    ]);

    const result = await extractPackageTar(tar, dest);

    expect(result.ok).toBe(true);
    const execMode = lstatSync(join(dest, 'bin', 'run.sh')).mode & 0o777;
    expect(execMode & 0o100).toBe(0o100); 
    const docMode = lstatSync(join(dest, 'README.md')).mode & 0o777;
    expect(docMode & 0o111).toBe(0); 
  });

  it('never carries setuid, setgid, or sticky bits from the archive into the extracted file', async () => {
    const dest = join(home, 'staging-mode-setuid');
    const tar = buildTar([
      { name: 'bin/evil', type: 'file', data: Buffer.from('x'), mode: 0o7755 },
    ]);

    const result = await extractPackageTar(tar, dest);

    expect(result.ok).toBe(true);
    const mode = lstatSync(join(dest, 'bin', 'evil')).mode & 0o7777;
    expect(mode & 0o7000).toBe(0); 
    expect(mode & 0o100).toBe(0o100); 
  });
});

describe('atomicSwapInstall', () => {
  function makeStagingWithMarker(
    dir: string,
    marker: ProvisionedMarker,
    fileContents = 'payload',
  ): void {
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, 'payload.txt'), fileContents);
    writeFileSync(join(dir, '.provisioned.json'), JSON.stringify(marker));
  }

  it('installs fresh when nothing exists yet', async () => {
    mkdirSync(join(home, 'mcp-servers'), { recursive: true });
    const installDir = join(home, 'mcp-servers', 'office');
    const staging = join(home, '.office.staging-1');
    const marker: ProvisionedMarker = {
      name: 'office',
      version: '1.0.0',
      platform: 'darwin-arm64',
      sha256: 'd'.repeat(64),
      installedAt: '2024-01-01T00:00:00.000Z',
    };
    makeStagingWithMarker(staging, marker);

    const outcome = await atomicSwapInstall(staging, installDir, marker);

    expect(outcome).toBe('installed');
    expect(readFileSync(join(installDir, 'payload.txt'), 'utf-8')).toBe('payload');
    expect(existsSync(staging)).toBe(false);
  });

  it('upgrades an existing older version by displacing it, never leaving the package uninstalled', async () => {
    const installDir = join(home, 'mcp-servers', 'office');
    const oldMarker: ProvisionedMarker = {
      name: 'office',
      version: '1.0.0',
      platform: 'darwin-arm64',
      sha256: 'd'.repeat(64),
      installedAt: '2024-01-01T00:00:00.000Z',
    };
    makeStagingWithMarker(installDir, oldMarker, 'old-payload');

    const staging = join(home, '.office.staging-2');
    const newMarker: ProvisionedMarker = { ...oldMarker, version: '2.0.0', sha256: 'e'.repeat(64) };
    makeStagingWithMarker(staging, newMarker, 'new-payload');

    const outcome = await atomicSwapInstall(staging, installDir, newMarker);

    expect(outcome).toBe('installed');
    expect(readFileSync(join(installDir, 'payload.txt'), 'utf-8')).toBe('new-payload');
    expect(await readProvisionedMarker(installDir)).toEqual(newMarker);
    expect(existsSync(staging)).toBe(false);
    const siblings = readdirSync(join(home, 'mcp-servers'));
    expect(siblings).toEqual(['office']);
  });

  it('resolves a concurrent-sync race: two racing installs of the SAME target version land as exactly one install, no corruption', async () => {
    mkdirSync(join(home, 'mcp-servers'), { recursive: true });
    const installDir = join(home, 'mcp-servers', 'office');
    const marker: ProvisionedMarker = {
      name: 'office',
      version: '1.0.0',
      platform: 'darwin-arm64',
      sha256: 'f'.repeat(64),
      installedAt: '2024-01-01T00:00:00.000Z',
    };
    const stagingA = join(home, '.office.staging-a');
    const stagingB = join(home, '.office.staging-b');
    makeStagingWithMarker(stagingA, marker, 'same-content');
    makeStagingWithMarker(stagingB, marker, 'same-content');

    const [outcomeA, outcomeB] = await Promise.all([
      atomicSwapInstall(stagingA, installDir, marker),
      atomicSwapInstall(stagingB, installDir, marker),
    ]);

    expect([outcomeA, outcomeB].sort()).toEqual(['installed', 'skipped']);
    expect(existsSync(installDir)).toBe(true);
    expect(readFileSync(join(installDir, 'payload.txt'), 'utf-8')).toBe('same-content');
    expect(await readProvisionedMarker(installDir)).toEqual(marker);
    expect(existsSync(stagingA)).toBe(false);
    expect(existsSync(stagingB)).toBe(false);
    const siblings = readdirSync(join(home, 'mcp-servers'));
    expect(siblings).toEqual(['office']);
  });

  describe('busy/idle swap deferral (issue #190)', () => {
    it('defers an upgrade swap while the daemon reports active tasks, then applies it once idle', async () => {
      const installDir = join(home, 'mcp-servers', 'office');
      const oldMarker: ProvisionedMarker = {
        name: 'office',
        version: '1.0.0',
        platform: 'darwin-arm64',
        sha256: 'd'.repeat(64),
        installedAt: '2024-01-01T00:00:00.000Z',
      };
      makeStagingWithMarker(installDir, oldMarker, 'old-payload');

      const staging = join(home, '.office.staging-busy');
      const newMarker: ProvisionedMarker = {
        ...oldMarker,
        version: '2.0.0',
        sha256: 'e'.repeat(64),
      };
      makeStagingWithMarker(staging, newMarker, 'new-payload');

      const busyOutcome = await atomicSwapInstall(staging, installDir, newMarker, async () => true);

      expect(busyOutcome).toBe('deferred');
      expect(readFileSync(join(installDir, 'payload.txt'), 'utf-8')).toBe('old-payload');
      expect(await readProvisionedMarker(installDir)).toEqual(oldMarker);
      expect(existsSync(staging)).toBe(true);

      const idleOutcome = await atomicSwapInstall(
        staging,
        installDir,
        newMarker,
        async () => false,
      );

      expect(idleOutcome).toBe('installed');
      expect(readFileSync(join(installDir, 'payload.txt'), 'utf-8')).toBe('new-payload');
      expect(await readProvisionedMarker(installDir)).toEqual(newMarker);
      expect(existsSync(staging)).toBe(false);
    });

    it('never defers a fresh install into an empty directory, even while the daemon is busy', async () => {
      mkdirSync(join(home, 'mcp-servers'), { recursive: true });
      const installDir = join(home, 'mcp-servers', 'office');
      const staging = join(home, '.office.staging-fresh-busy');
      const marker: ProvisionedMarker = {
        name: 'office',
        version: '1.0.0',
        platform: 'darwin-arm64',
        sha256: 'd'.repeat(64),
        installedAt: '2024-01-01T00:00:00.000Z',
      };
      makeStagingWithMarker(staging, marker);

      const outcome = await atomicSwapInstall(staging, installDir, marker, async () => true);

      expect(outcome).toBe('installed');
      expect(readFileSync(join(installDir, 'payload.txt'), 'utf-8')).toBe('payload');
    });
  });

  describe('unmarked pre-0.7.0 content (issue #190)', () => {
    it('preserves an unmarked directory aside instead of deleting it', async () => {
      const installDir = join(home, 'mcp-servers', 'office');
      mkdirSync(installDir, { recursive: true });
      writeFileSync(join(installDir, 'user-edited.txt'), 'please do not delete me');

      const staging = join(home, '.office.staging-legacy');
      const marker: ProvisionedMarker = {
        name: 'office',
        version: '1.0.0',
        platform: 'darwin-arm64',
        sha256: 'd'.repeat(64),
        installedAt: '2024-01-01T00:00:00.000Z',
      };
      makeStagingWithMarker(staging, marker, 'new-payload');

      const outcome = await atomicSwapInstall(staging, installDir, marker);

      expect(outcome).toBe('installed');
      expect(readFileSync(join(installDir, 'payload.txt'), 'utf-8')).toBe('new-payload');
      const preserved = `${installDir}.pre-0.7.0`;
      expect(existsSync(preserved)).toBe(true);
      expect(readFileSync(join(preserved, 'user-edited.txt'), 'utf-8')).toBe(
        'please do not delete me',
      );
    });

    it('reports the preserved path through onLegacyContentPreserved instead of only console.warn', async () => {
      const installDir = join(home, 'mcp-servers', 'office');
      mkdirSync(installDir, { recursive: true });
      writeFileSync(join(installDir, 'user-edited.txt'), 'please do not delete me');

      const staging = join(home, '.office.staging-legacy-2');
      const marker: ProvisionedMarker = {
        name: 'office',
        version: '1.0.0',
        platform: 'darwin-arm64',
        sha256: 'd'.repeat(64),
        installedAt: '2024-01-01T00:00:00.000Z',
      };
      makeStagingWithMarker(staging, marker, 'new-payload');

      const onLegacyContentPreserved = vi.fn();
      const outcome = await atomicSwapInstall(
        staging,
        installDir,
        marker,
        undefined,
        onLegacyContentPreserved,
      );

      expect(outcome).toBe('installed');
      expect(onLegacyContentPreserved).toHaveBeenCalledTimes(1);
      expect(onLegacyContentPreserved).toHaveBeenCalledWith(`${installDir}.pre-0.7.0`);
    });
  });
});

describe('ProvisioningStatus.restartPending / preservedLegacyPaths (issue #191)', () => {
  it('defaults restartPending to false and preservedLegacyPaths to empty', () => {
    const status = getProvisioningStatus();
    expect(status.restartPending).toBe(false);
    expect(status.preservedLegacyPaths).toEqual([]);
  });

  it('setProvisioningRestartPending flips the status field for daemon-manager.ts to drive', () => {
    setProvisioningRestartPending(true);
    expect(getProvisioningStatus().restartPending).toBe(true);

    setProvisioningRestartPending(false);
    expect(getProvisioningStatus().restartPending).toBe(false);
  });

  it('a package sync that hits the unmarked-legacy-content path surfaces the preserved path in the status, deduplicated', async () => {
    const auth = { apiBaseUrl: 'https://goosar.example', token: 'gsl_test', workspaceId: 'ws_1' };
    const installDir = join(home, '.hermes', 'mcp-servers', 'office');
    mkdirSync(installDir, { recursive: true });
    writeFileSync(join(installDir, 'legacy.txt'), 'keep me');

    const tar = buildTar([{ name: 'SKILL.md', type: 'file', data: Buffer.from('v1') }]);
    const pkg = manifestPackage({ sha256: sha256Hex(tar) });
    const fetchImpl: AuthedFetch = async () => new Response(new Uint8Array(tar), { status: 200 });

    const state = await syncOnePackage(ctx(), pkg, auth, {
      fetchImpl,
      decompress: async (input) => input,
      onLegacyContentPreserved: () => {
        // syncOnePackage itself does not touch module status — this proves
        // the callback wiring reaches atomicSwapInstall from syncOnePackage's
        // deps, independent of runSync's own wiring (covered below via
        // getProvisioningStatus after a full sync pass).
      },
    });
    expect(state).toBe('ok');
    expect(existsSync(`${installDir}.pre-0.7.0`)).toBe(true);
  });

  it('a full sync pass (runSync) records the preserved path into getProvisioningStatus()', async () => {
    setProvisioningCredentials('https://goosar.example', 'gsl_test');
    setProvisioningWorkspaceId('ws_1');

    const installDir = join(home, '.hermes', 'mcp-servers', 'office');
    mkdirSync(installDir, { recursive: true });
    writeFileSync(join(installDir, 'legacy.txt'), 'keep me');

    const tar = buildTar([{ name: 'SKILL.md', type: 'file', data: Buffer.from('v1') }]);
    const pkg = manifestPackage({ sha256: sha256Hex(tar) });
    const fetchImpl: AuthedFetch = async (input) => {
      const url = String(input);
      if (url.includes('/manifest')) {
        return new Response(JSON.stringify({ schemaVersion: 1, packages: [pkg] }), { status: 200 });
      }
      return new Response(new Uint8Array(tar), { status: 200 });
    };

    await syncProvisionedPackages({ ctx: ctx(), fetchImpl, decompress: async (i) => i });

    const status = getProvisioningStatus();
    expect(status.preservedLegacyPaths).toEqual([`${installDir}.pre-0.7.0`]);
  });
});

describe('probeProvisioningReachability (issue #191)', () => {
  it('carries restartPending and preservedLegacyPaths through onto the reachability fact', async () => {
    setProvisioningCredentials('https://goosar.example', 'gsl_test');
    setProvisioningWorkspaceId('ws_1');

    const tar = buildTar([{ name: 'SKILL.md', type: 'file', data: Buffer.from('v1') }]);
    const pkg = manifestPackage({ sha256: sha256Hex(tar) });
    const fetchImpl: AuthedFetch = async (input) => {
      const url = String(input);
      if (url.includes('/manifest')) {
        return new Response(JSON.stringify({ schemaVersion: 1, packages: [pkg] }), { status: 200 });
      }
      return new Response(new Uint8Array(tar), { status: 200 });
    };
    await syncProvisionedPackages({ ctx: ctx(), fetchImpl, decompress: async (i) => i });
    setProvisioningRestartPending(true);

    const fact = await probeProvisioningReachability();
    expect(fact.kind).toBe('status');
    if (fact.kind === 'status') {
      expect(fact.restartPending).toBe(true);
      expect(fact.preservedLegacyPaths).toEqual([]);
    }
  });

  it('carries removedPackages through the UNCONFIGURED fact — issue #191 pass-2 M (disable the only pin: package removed AND the removal row stays visible)', async () => {
    setProvisioningCredentials('https://goosar.example', 'gsl_test');
    setProvisioningWorkspaceId('ws_1');

    const dir = join(packageFamilyRoot(ctx(), 'skill'), 'office-docx');
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, 'payload.txt'), 'bytes');
    writeFileSync(
      join(dir, '.provisioned.json'),
      JSON.stringify({
        name: 'office-docx',
        version: '1.0.0',
        platform: 'darwin-arm64',
        sha256: 'e'.repeat(64),
        installedAt: '2026-01-01T00:00:00.000Z',
      }),
    );

    const fetchImpl: AuthedFetch = async () =>
      new Response(
        JSON.stringify({
          schemaVersion: 1,
          packages: [],
          revokedPackages: ['skill:office-docx@1.0.0'],
        }),
        { status: 200 },
      );
    await syncProvisionedPackages({ ctx: ctx(), fetchImpl });

    expect(existsSync(dir)).toBe(false);

    const fact = await probeProvisioningReachability();
    expect(fact.kind).toBe('unconfigured');
    if (fact.kind === 'unconfigured') {
      expect(fact.removedPackages).toEqual([
        { name: 'office-docx', type: 'skill', version: '1.0.0', state: 'removed' },
      ]);
    }
  });
});

function manifestPackage(overrides: Partial<PackageManifest> = {}): PackageManifest {
  const tar = buildTar([{ name: 'SKILL.md', type: 'file', data: Buffer.from('# office\n') }]);
  return {
    schemaVersion: 1,
    name: 'office',
    version: '1.0.0',
    type: 'mcp-server',
    platform: 'darwin-arm64',
    sha256: sha256Hex(tar),
    size: tar.length,
    requires: [],
    ...overrides,
  };
}

describe('syncOnePackage', () => {
  const auth = { apiBaseUrl: 'https://goosar.example', token: 'gsl_test', workspaceId: 'ws_1' };

  it('rejects a corrupt/tampered blob on sha256 mismatch: nothing is installed and staging is cleaned', async () => {
    const tar = buildTar([{ name: 'SKILL.md', type: 'file', data: Buffer.from('# office\n') }]);
    const pkg = manifestPackage({ sha256: sha256Hex(tar) });
    const corrupted = Buffer.concat([tar, Buffer.from('EXTRA')]);
    const fetchImpl: AuthedFetch = async () =>
      new Response(new Uint8Array(corrupted), { status: 200 });

    const state = await syncOnePackage(ctx(), pkg, auth, {
      fetchImpl,
      decompress: async (input) => input,
    });

    expect(state).toBe('fail');
    const installDir = packageInstallDir(ctx(), pkg);
    expect(existsSync(installDir)).toBe(false);
    const familyRoot = join(home, '.hermes', 'mcp-servers');
    const leftovers = existsSync(familyRoot) ? readdirSync(familyRoot) : [];
    expect(leftovers).toEqual([]);
  });

  it('installs on first sync, then performs a version-aware upgrade on a version bump', async () => {
    const tarV1 = buildTar([{ name: 'SKILL.md', type: 'file', data: Buffer.from('v1') }]);
    const pkgV1 = manifestPackage({ version: '1.0.0', sha256: sha256Hex(tarV1) });
    const fetchV1: AuthedFetch = async () => new Response(new Uint8Array(tarV1), { status: 200 });

    const stateV1 = await syncOnePackage(ctx(), pkgV1, auth, {
      fetchImpl: fetchV1,
      decompress: async (input) => input,
    });
    expect(stateV1).toBe('ok');
    const installDir = packageInstallDir(ctx(), pkgV1);
    expect(readFileSync(join(installDir, 'SKILL.md'), 'utf-8')).toBe('v1');
    expect((await readProvisionedMarker(installDir))?.version).toBe('1.0.0');

    const tarV2 = buildTar([{ name: 'SKILL.md', type: 'file', data: Buffer.from('v2') }]);
    const pkgV2 = manifestPackage({ version: '2.0.0', sha256: sha256Hex(tarV2) });
    const fetchV2: AuthedFetch = async () => new Response(new Uint8Array(tarV2), { status: 200 });

    const stateV2 = await syncOnePackage(ctx(), pkgV2, auth, {
      fetchImpl: fetchV2,
      decompress: async (input) => input,
    });
    expect(stateV2).toBe('ok');
    expect(readFileSync(join(installDir, 'SKILL.md'), 'utf-8')).toBe('v2');
    expect((await readProvisionedMarker(installDir))?.version).toBe('2.0.0');
  });

  it('skips re-downloading when the installed marker already matches the manifest (never a bare existsSync skip)', async () => {
    const tar = buildTar([{ name: 'SKILL.md', type: 'file', data: Buffer.from('v1') }]);
    const pkg = manifestPackage({ sha256: sha256Hex(tar) });
    const fetchImpl = vi.fn<AuthedFetch>(
      async () => new Response(new Uint8Array(tar), { status: 200 }),
    );

    await syncOnePackage(ctx(), pkg, auth, { fetchImpl, decompress: async (i) => i });
    expect(fetchImpl).toHaveBeenCalledTimes(1);

    const state = await syncOnePackage(ctx(), pkg, auth, {
      fetchImpl,
      decompress: async (i) => i,
    });
    expect(state).toBe('ok');
    expect(fetchImpl).toHaveBeenCalledTimes(1);
  });

  it('refuses to buffer/install a correctly-hashed blob that vastly exceeds the manifest-declared size', async () => {
    const oversizedTar = buildTar([
      { name: 'payload.bin', type: 'file', data: Buffer.alloc(64 * 1024 * 1024, 1) },
    ]);
    const pkg = manifestPackage({ size: 1024, sha256: sha256Hex(oversizedTar) });
    const fetchImpl: AuthedFetch = async () =>
      new Response(new Uint8Array(oversizedTar), { status: 200 });

    const state = await syncOnePackage(ctx(), pkg, auth, {
      fetchImpl,
      decompress: async (input) => input,
    });

    expect(state).toBe('fail');
    expect(existsSync(packageInstallDir(ctx(), pkg))).toBe(false);
  });

  it('surfaces a familyRoot mkdir failure as a per-package fail instead of throwing', async () => {
    const pkg = manifestPackage();
    writeFileSync(join(home, '.hermes'), 'not a directory');
    const fetchImpl: AuthedFetch = async () => new Response(new Uint8Array(0));

    await expect(
      syncOnePackage(ctx(), pkg, auth, { fetchImpl, decompress: async (i) => i }),
    ).resolves.toBe('fail');
  });

  describe('resumable downloads (issue #190)', () => {
    function rangeHeaderOf(init: RequestInit | undefined): string | undefined {
      return (init?.headers as Record<string, string> | undefined)?.Range;
    }

    it('resumes via an HTTP Range request after a mid-stream network error, within the same sync attempt', async () => {
      const tar = buildTar([{ name: 'payload.bin', type: 'file', data: Buffer.alloc(4096, 9) }]);
      const pkg = manifestPackage({ sha256: sha256Hex(tar), size: tar.length });
      const partialLength = 1500;
      let callCount = 0;
      const fetchImpl: AuthedFetch = async (_url, init) => {
        callCount += 1;
        if (callCount === 1) {
          expect(rangeHeaderOf(init)).toBeUndefined();
          let pulls = 0;
          const stream = new ReadableStream<Uint8Array>({
            pull(controller) {
              pulls += 1;
              if (pulls === 1) {
                controller.enqueue(tar.subarray(0, partialLength));
              } else {
                controller.error(new Error('simulated network drop'));
              }
            },
          });
          return new Response(stream, {
            status: 200,
            headers: { 'content-length': String(tar.length) },
          });
        }
        expect(rangeHeaderOf(init)).toBe(`bytes=${partialLength}-`);
        return new Response(new Uint8Array(tar.subarray(partialLength)), {
          status: 206,
        });
      };

      const state = await syncOnePackage(ctx(), pkg, auth, {
        fetchImpl,
        decompress: async (i) => i,
      });

      expect(state).toBe('ok');
      expect(callCount).toBe(2);
      expect(readFileSync(join(packageInstallDir(ctx(), pkg), 'payload.bin'))).toEqual(
        Buffer.alloc(4096, 9),
      );
    });

    it('keeps partial bytes across a failed sync pass so the NEXT pass resumes instead of restarting from zero, and settles into fail with a stable reasonCode rather than retrying forever', async () => {
      const tar = buildTar([{ name: 'big.bin', type: 'file', data: Buffer.alloc(4096, 7) }]);
      const pkg = manifestPackage({ sha256: sha256Hex(tar), size: tar.length });
      const partialLength = 1500;

      let pass1Calls = 0;
      const flakyFetch: AuthedFetch = async (_url, init) => {
        pass1Calls += 1;
        if (pass1Calls === 1) {
          expect(rangeHeaderOf(init)).toBeUndefined();
          let pulls = 0;
          const stream = new ReadableStream<Uint8Array>({
            pull(controller) {
              pulls += 1;
              if (pulls === 1) {
                controller.enqueue(tar.subarray(0, partialLength));
              } else {
                controller.error(new Error('simulated network drop'));
              }
            },
          });
          return new Response(stream, {
            status: 200,
            headers: { 'content-length': String(tar.length) },
          });
        }
        expect(rangeHeaderOf(init)).toBe(`bytes=${partialLength}-`);
        return new Response('service unavailable', { status: 503 });
      };

      let reasonCode: string | undefined;
      const state1 = await syncOnePackage(ctx(), pkg, auth, {
        fetchImpl: flakyFetch,
        decompress: async (i) => i,
        onReasonCode: (code) => {
          reasonCode = code;
        },
      });

      expect(state1).toBe('fail');
      expect(reasonCode).toBe('provisioning_download_failed');
      expect(pass1Calls).toBeGreaterThan(1);
      expect(pass1Calls).toBeLessThan(10);
      expect(existsSync(packageInstallDir(ctx(), pkg))).toBe(false);

      let pass2Calls = 0;
      const healthyFetch: AuthedFetch = async (_url, init) => {
        pass2Calls += 1;
        expect(rangeHeaderOf(init)).toBe(`bytes=${partialLength}-`);
        return new Response(new Uint8Array(tar.subarray(partialLength)), {
          status: 206,
        });
      };

      const state2 = await syncOnePackage(ctx(), pkg, auth, {
        fetchImpl: healthyFetch,
        decompress: async (i) => i,
      });

      expect(state2).toBe('ok');
      expect(pass2Calls).toBe(1);
      expect(readFileSync(join(packageInstallDir(ctx(), pkg), 'big.bin'))).toEqual(
        Buffer.alloc(4096, 7),
      );
    });

    it('surfaces live byte progress into getProvisioningStatus while a package downloads', async () => {
      setProvisioningCredentials('https://goosar.example', 'gsl_test');
      setProvisioningWorkspaceId('ws_1');
      const tar = buildTar([{ name: 'payload.bin', type: 'file', data: Buffer.alloc(2048, 3) }]);
      const pkg = manifestPackage({
        name: 'big-skill',
        type: 'skill',
        sha256: sha256Hex(tar),
        size: tar.length,
      });

      const observedProgress: Array<{ downloaded: number; total: number }> = [];
      const fetchImpl: AuthedFetch = async (input) => {
        const url = String(input);
        if (url.includes('/manifest')) {
          return new Response(JSON.stringify({ schemaVersion: 1, packages: [pkg] }), {
            status: 200,
          });
        }
        const stream = new ReadableStream<Uint8Array>({
          start(controller) {
            controller.enqueue(tar.subarray(0, 1024));
            controller.enqueue(tar.subarray(1024));
            controller.close();
          },
        });
        return new Response(stream, { status: 200 });
      };

      const pollUntilProgressSeen = (async () => {
        for (let i = 0; i < 200; i += 1) {
          const pkgStatus = getProvisioningStatus().packages.find((p) => p.name === 'big-skill');
          if (pkgStatus?.bytesDownloaded !== undefined) {
            observedProgress.push({
              downloaded: pkgStatus.bytesDownloaded,
              total: pkgStatus.bytesTotal ?? 0,
            });
            break;
          }
          await new Promise((resolve) => setTimeout(resolve, 0));
        }
      })();

      await Promise.all([
        syncProvisionedPackages({ ctx: ctx(), fetchImpl, decompress: async (i) => i }),
        pollUntilProgressSeen,
      ]);

      const finalStatus = getProvisioningStatus();
      expect(finalStatus.state).toBe('ok');
      const finalPkgStatus = finalStatus.packages.find((p) => p.name === 'big-skill');
      expect(finalPkgStatus?.bytesDownloaded).toBe(tar.length);
      expect(finalPkgStatus?.bytesTotal).toBe(tar.length);
    });
  });
});

describe('syncProvisionedPackages', () => {
  it('is a no-op until workspace/login credentials are known', async () => {
    const fetchImpl = vi.fn<AuthedFetch>(async () => new Response('{}'));
    await syncProvisionedPackages({ ctx: ctx(), fetchImpl });
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it('memoizes concurrent calls into one in-flight run', async () => {
    setProvisioningCredentials('https://goosar.example', 'gsl_test');
    setProvisioningWorkspaceId('ws_1');
    let manifestCalls = 0;
    const fetchImpl: AuthedFetch = async (input) => {
      if (String(input).includes('/manifest')) {
        manifestCalls += 1;
        await new Promise((resolve) => setTimeout(resolve, 10));
        return new Response(JSON.stringify({ schemaVersion: 1, packages: [] }), {
          status: 200,
        });
      }
      return new Response('{}', { status: 200 });
    };

    await Promise.all([
      syncProvisionedPackages({ ctx: ctx(), fetchImpl }),
      syncProvisionedPackages({ ctx: ctx(), fetchImpl }),
    ]);

    expect(manifestCalls).toBe(1);
  });
});

describe('baked-in store fallback (issues #153/#156)', () => {
  let storeDir: string;

  function bakePackage(
    name: string,
    contents = `# ${name}\n`,
    overrides: Partial<PackageManifest> = {},
  ): PackageManifest {
    const tar = buildTar([{ name: 'SKILL.md', type: 'file', data: Buffer.from(contents) }]);
    const pkg = manifestPackage({
      name,
      sha256: sha256Hex(tar),
      size: tar.length,
      ...overrides,
    });
    const token = pkg.platform === '*' ? 'any' : pkg.platform;
    const base = `${pkg.name}-${pkg.version}-${token}`;
    writeFileSync(join(storeDir, `${base}.tar.zst`), tar);
    writeFileSync(join(storeDir, `${base}.manifest.json`), JSON.stringify(pkg));
    return pkg;
  }

  beforeEach(() => {
    storeDir = join(home, 'baked-store');
    mkdirSync(storeDir, { recursive: true });
    setProvisioningCredentials('https://goosar.example', 'gsl_test');
    setProvisioningWorkspaceId('ws_1');
  });

  it('readBakedStoreManifest returns platform-matching pairs and skips broken entries', async () => {
    const match = bakePackage('office');
    bakePackage('other-arch', '# other\n', { platform: 'win-x64' });
    writeFileSync(
      join(storeDir, 'lonely-1.0.0-any.manifest.json'),
      JSON.stringify(manifestPackage({ name: 'lonely', platform: '*' })),
    );
    writeFileSync(join(storeDir, 'junk-1.0.0-any.manifest.json'), '{nope');

    const baked = await readBakedStoreManifest(storeDir, 'darwin-arm64');
    expect(baked.map((b) => b.pkg.name)).toEqual([match.name]);
    expect(existsSync(baked[0].blobPath)).toBe(true);
  });

  it('readBakedStoreManifest keeps only the first entry per type:name — a second version must not clobber the blob mapping', async () => {
    bakePackage('office', '# v1\n', { version: '1.0.0' });
    bakePackage('office', '# v2\n', { version: '2.0.0' });
    const baked = await readBakedStoreManifest(storeDir, 'darwin-arm64');
    expect(baked).toHaveLength(1);
    expect(baked[0].pkg.version).toBe('1.0.0');
  });

  it('readBakedStoreManifest returns [] for a missing directory', async () => {
    expect(await readBakedStoreManifest(join(home, 'nope'), 'darwin-arm64')).toEqual([]);
  });

  it('installs from the baked-in store when the server is unconfigured (503)', async () => {
    const pkg = bakePackage('office');
    const fetchImpl = vi.fn<AuthedFetch>(async () => new Response('', { status: 503 }));

    await syncProvisionedPackages({
      ctx: ctx(),
      fetchImpl,
      decompress: async (i) => i,
      bakedStoreDir: storeDir,
    });

    const installDir = packageInstallDir(ctx(), pkg);
    expect(existsSync(join(installDir, 'SKILL.md'))).toBe(true);
    expect(fetchImpl).toHaveBeenCalledTimes(1);
    const status = getProvisioningStatus();
    expect(status.state).toBe('ok');
    expect(status.summary.installed).toBe(1);
  });

  it('installs from the baked-in store when the server serves an empty catalog', async () => {
    const pkg = bakePackage('office');
    const fetchImpl: AuthedFetch = async () =>
      new Response(JSON.stringify({ schemaVersion: 1, packages: [] }), { status: 200 });

    await syncProvisionedPackages({
      ctx: ctx(),
      fetchImpl,
      decompress: async (i) => i,
      bakedStoreDir: storeDir,
    });

    expect(existsSync(join(packageInstallDir(ctx(), pkg), 'SKILL.md'))).toBe(true);
  });

  it('never resurrects a package the empty catalog explicitly revokes', async () => {
    const pkg = bakePackage('office');
    const fetchImpl: AuthedFetch = async () =>
      new Response(
        JSON.stringify({
          schemaVersion: 1,
          packages: [],
          revokedPackages: [`${pkg.type}:${pkg.name}@${pkg.version}`],
        }),
        { status: 200 },
      );

    await syncProvisionedPackages({
      ctx: ctx(),
      fetchImpl,
      decompress: async (i) => i,
      bakedStoreDir: storeDir,
    });

    expect(existsSync(packageInstallDir(ctx(), pkg))).toBe(false);
  });

  it('a non-empty server catalog wins — the baked-in store is not consulted', async () => {
    bakePackage('office', '# BAKED, must not install\n');
    const tar = buildTar([{ name: 'SKILL.md', type: 'file', data: Buffer.from('# SERVER\n') }]);
    const serverPkg = manifestPackage({ name: 'office', sha256: sha256Hex(tar), size: tar.length });
    const fetchImpl: AuthedFetch = async (input) => {
      if (String(input).includes('/manifest')) {
        return new Response(JSON.stringify({ schemaVersion: 1, packages: [serverPkg] }), {
          status: 200,
        });
      }
      return new Response(new Uint8Array(tar), { status: 200 });
    };

    await syncProvisionedPackages({
      ctx: ctx(),
      fetchImpl,
      decompress: async (i) => i,
      bakedStoreDir: storeDir,
    });

    const installed = readFileSync(join(packageInstallDir(ctx(), serverPkg), 'SKILL.md'), 'utf-8');
    expect(installed).toBe('# SERVER\n');
  });

  it("no baked store + unconfigured server keeps the pre-existing 'not configured' behavior", async () => {
    const fetchImpl: AuthedFetch = async () => new Response('', { status: 503 });
    await syncProvisionedPackages({ ctx: ctx(), fetchImpl, bakedStoreDir: null });
    const status = getProvisioningStatus();
    expect(status.configured).toBe(false);
    expect(status.reasonCode).toBe('provisioning_not_configured');
  });

  it('syncOnePackage refuses a tampered baked-in blob on sha256 mismatch', async () => {
    const pkg = bakePackage('office');
    const token = pkg.platform === '*' ? 'any' : pkg.platform;
    const blobPath = join(storeDir, `${pkg.name}-${pkg.version}-${token}.tar.zst`);
    writeFileSync(blobPath, 'tampered bytes');

    const auth = { apiBaseUrl: 'https://goosar.example', token: 'gsl_test', workspaceId: 'ws_1' };
    const state = await syncOnePackage(ctx(), pkg, auth, {
      fetchImpl: async () => new Response('', { status: 500 }),
      decompress: async (i) => i,
      localBlobPath: blobPath,
    });
    expect(state).toBe('fail');
    expect(existsSync(packageInstallDir(ctx(), pkg))).toBe(false);
  });
});

function pkgWithBlob(
  overrides: Partial<PackageManifest> & Pick<PackageManifest, 'name' | 'type'>,
): { pkg: PackageManifest; tar: Buffer } {
  const tar = buildTar([
    { name: 'file.txt', type: 'file', data: Buffer.from(`${overrides.name}-payload`) },
  ]);
  const pkg = manifestPackage({
    version: '1.0.0',
    ...overrides,
    sha256: sha256Hex(tar),
    size: tar.length,
  });
  return { pkg, tar };
}

function manifestAndBlobFetch(
  packages: PackageManifest[],
  blobs: ReadonlyMap<string, Buffer>,
): AuthedFetch {
  return async (input) => {
    const url = String(input);
    if (url.includes('/manifest')) {
      return new Response(JSON.stringify({ schemaVersion: 1, packages }), {
        status: 200,
      });
    }
    const match = url.match(/\/blob\/([^/]+)\/([^/?]+)/);
    if (match) {
      const key = `${decodeURIComponent(match[1])}@${decodeURIComponent(match[2])}`;
      const tar = blobs.get(key);
      if (!tar) return new Response('not found', { status: 404 });
      return new Response(new Uint8Array(tar), { status: 200 });
    }
    return new Response('{}', { status: 200 });
  };
}

describe('getProvisioningStatus: per-type progress (issue #188)', () => {
  it('starts idle, configured, and all-zero before any sync has run', () => {
    const status = getProvisioningStatus();
    expect(status.state).toBe('idle');
    expect(status.configured).toBe(true);
    expect(status.byType).toEqual({
      skill: { installed: 0, total: 0, failed: 0 },
      'mcp-server': { installed: 0, total: 0, failed: 0 },
      runtime: { installed: 0, total: 0, failed: 0 },
    });
    expect(status.summary).toEqual({ installed: 0, total: 0, failed: 0 });
    expect(status.packages).toEqual([]);
  });

  it('counts installed/total/failed per package type independently: skills done, one MCP server failed', async () => {
    setProvisioningCredentials('https://goosar.example', 'gsl_test');
    setProvisioningWorkspaceId('ws_1');

    const office = pkgWithBlob({ name: 'office', type: 'skill' });
    const postgres = pkgWithBlob({ name: 'postgres', type: 'mcp-server' });
    const brokenMcp = pkgWithBlob({ name: 'broken-mcp', type: 'mcp-server' });

    const blobs = new Map([
      [`${office.pkg.name}@${office.pkg.version}`, office.tar],
      [`${postgres.pkg.name}@${postgres.pkg.version}`, postgres.tar],
      // brokenMcp deliberately has no matching blob entry -> 404 -> "fail".
    ]);
    const fetchImpl = manifestAndBlobFetch([office.pkg, postgres.pkg, brokenMcp.pkg], blobs);

    await syncProvisionedPackages({ ctx: ctx(), fetchImpl, decompress: async (i) => i });

    const status = getProvisioningStatus();
    expect(status.configured).toBe(true);
    expect(status.state).toBe('fail');
    expect(status.byType.skill).toEqual({ installed: 1, total: 1, failed: 0 });
    expect(status.byType['mcp-server']).toEqual({ installed: 1, total: 2, failed: 1 });
    expect(status.byType.runtime).toEqual({ installed: 0, total: 0, failed: 0 });
    expect(status.summary).toEqual({ installed: 2, total: 3, failed: 1 });
    const failingPackage = status.packages.find((p) => p.name === 'broken-mcp');
    expect(failingPackage?.state).toBe('fail');
  });

  it('reports state: "ok" once every package across all three types installs', async () => {
    setProvisioningCredentials('https://goosar.example', 'gsl_test');
    setProvisioningWorkspaceId('ws_1');

    const skill = pkgWithBlob({ name: 'office', type: 'skill' });
    const mcp = pkgWithBlob({ name: 'postgres', type: 'mcp-server' });
    const runtime = pkgWithBlob({ name: 'python', type: 'runtime' });
    const blobs = new Map([
      [`${skill.pkg.name}@${skill.pkg.version}`, skill.tar],
      [`${mcp.pkg.name}@${mcp.pkg.version}`, mcp.tar],
      [`${runtime.pkg.name}@${runtime.pkg.version}`, runtime.tar],
    ]);
    const fetchImpl = manifestAndBlobFetch([skill.pkg, mcp.pkg, runtime.pkg], blobs);

    await syncProvisionedPackages({ ctx: ctx(), fetchImpl, decompress: async (i) => i });

    const status = getProvisioningStatus();
    expect(status.state).toBe('ok');
    expect(status.reasonCode).toBeUndefined();
    expect(status.byType.skill).toEqual({ installed: 1, total: 1, failed: 0 });
    expect(status.byType['mcp-server']).toEqual({ installed: 1, total: 1, failed: 0 });
    expect(status.byType.runtime).toEqual({ installed: 1, total: 1, failed: 0 });
    expect(status.summary).toEqual({ installed: 3, total: 3, failed: 0 });
  });
});

describe('getProvisioningStatus: configured semantics (issue #188)', () => {
  it('is configured:false — never a failure — when the manifest endpoint 503s (store not configured)', async () => {
    setProvisioningCredentials('https://goosar.example', 'gsl_test');
    setProvisioningWorkspaceId('ws_1');
    const fetchImpl: AuthedFetch = async () => new Response('service unavailable', { status: 503 });

    await syncProvisionedPackages({ ctx: ctx(), fetchImpl });

    const status = getProvisioningStatus();
    expect(status.configured).toBe(false);
    expect(status.state).not.toBe('fail');
    expect(status.packages).toEqual([]);
  });

  it('is configured:false — never a failure — when the catalog resolves empty', async () => {
    setProvisioningCredentials('https://goosar.example', 'gsl_test');
    setProvisioningWorkspaceId('ws_1');
    const fetchImpl: AuthedFetch = async () =>
      new Response(JSON.stringify({ schemaVersion: 1, packages: [] }), {
        status: 200,
      });

    await syncProvisionedPackages({ ctx: ctx(), fetchImpl });

    const status = getProvisioningStatus();
    expect(status.configured).toBe(false);
    expect(status.state).not.toBe('fail');
  });

  it('is a real failure when the manifest endpoint errors for a reason OTHER than "not configured"', async () => {
    setProvisioningCredentials('https://goosar.example', 'gsl_test');
    setProvisioningWorkspaceId('ws_1');
    const fetchImpl: AuthedFetch = async () => new Response('boom', { status: 500 });

    await syncProvisionedPackages({ ctx: ctx(), fetchImpl });

    expect(getProvisioningStatus().state).toBe('fail');
  });
});

describe('retryProvisioning (issue #188)', () => {
  it('actually re-runs a sync that a memoized in-flight call would have skipped', async () => {
    setProvisioningCredentials('https://goosar.example', 'gsl_test');
    setProvisioningWorkspaceId('ws_1');
    let manifestCalls = 0;
    const fetchImpl: AuthedFetch = async (input) => {
      if (String(input).includes('/manifest')) {
        manifestCalls += 1;
        await new Promise((resolve) => setTimeout(resolve, 20));
        return new Response(JSON.stringify({ schemaVersion: 1, packages: [] }), {
          status: 200,
        });
      }
      return new Response('{}', { status: 200 });
    };

    const first = syncProvisionedPackages({ ctx: ctx(), fetchImpl });
    const second = syncProvisionedPackages({ ctx: ctx(), fetchImpl });
    expect(second).toBe(first);

    const retried = retryProvisioning({ ctx: ctx(), fetchImpl });

    await Promise.all([first, second, retried]);
    expect(manifestCalls).toBe(2);
  });
});

describe('bindProvisioningWorkspace / bindProvisioningCredentials (issue #188)', () => {
  it('kicks an immediate sync when the workspace id arrives AFTER credentials are already known', async () => {
    setProvisioningCredentials('https://goosar.example', 'gsl_test');
    const fetchImpl = vi.fn<AuthedFetch>(
      async () =>
        new Response(JSON.stringify({ schemaVersion: 1, packages: [] }), {
          status: 200,
        }),
    );

    await bindProvisioningWorkspace('ws_1', { ctx: ctx(), fetchImpl });

    expect(fetchImpl).toHaveBeenCalledTimes(1);
  });

  it('is a harmless fetch-free no-op when credentials are not yet known', async () => {
    const fetchImpl = vi.fn<AuthedFetch>(async () => new Response('{}'));

    await bindProvisioningWorkspace('ws_1', { ctx: ctx(), fetchImpl });

    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it('kicks an immediate sync when credentials arrive AFTER the workspace id is already known (the reverse order)', async () => {
    setProvisioningWorkspaceId('ws_1');
    const fetchImpl = vi.fn<AuthedFetch>(
      async () =>
        new Response(JSON.stringify({ schemaVersion: 1, packages: [] }), {
          status: 200,
        }),
    );

    await bindProvisioningCredentials('https://goosar.example', 'gsl_test', {
      ctx: ctx(),
      fetchImpl,
    });

    expect(fetchImpl).toHaveBeenCalledTimes(1);
  });
});

describe('nodeZstdDecompress / defaultZstdDecompress (blocker #189-1: no external zstd binary)', () => {
  it("nodeZstdDecompress round-trips a zstd-compressed buffer using only Node's built-in zlib codec", async () => {
    const raw = Buffer.from("hello from node's built-in zstd\n".repeat(50));
    const compressed = zstdCompressSync(raw);

    await expect(nodeZstdDecompress(compressed)).resolves.toEqual(raw);
  });

  it('defaultZstdDecompress decompresses a genuine .tar.zst end-to-end with NO `zstd` binary anywhere on PATH', async () => {
    const tar = buildTar([{ name: 'SKILL.md', type: 'file', data: Buffer.from('# office\n') }]);
    const compressed = zstdCompressSync(tar);

    const emptyPathDir = mkdtempSync(join(tmpdir(), 'goosar-empty-path-'));
    const originalPath = process.env.PATH;
    process.env.PATH = emptyPathDir;
    try {
      await expect(defaultZstdDecompress(compressed)).resolves.toEqual(tar);
    } finally {
      process.env.PATH = originalPath;
      rmSync(emptyPathDir, { recursive: true, force: true });
    }
  });

  it('syncProvisionedPackages installs a package end-to-end through the SHIPPED default decompressor — no injected `decompress` override, no `zstd` on PATH', async () => {
    setProvisioningCredentials('https://goosar.example', 'gsl_test');
    setProvisioningWorkspaceId('ws_1');

    const tar = buildTar([{ name: 'SKILL.md', type: 'file', data: Buffer.from('# office\n') }]);
    const compressed = zstdCompressSync(tar);
    const pkg: PackageManifest = {
      schemaVersion: 1,
      name: 'office',
      version: '1.0.0',
      type: 'mcp-server',
      platform: 'darwin-arm64',
      sha256: sha256Hex(compressed),
      size: compressed.length,
      requires: [],
    };
    const fetchImpl: AuthedFetch = async (input) => {
      if (String(input).includes('/manifest')) {
        return new Response(JSON.stringify({ schemaVersion: 1, packages: [pkg] }), { status: 200 });
      }
      return new Response(new Uint8Array(compressed), { status: 200 });
    };

    const emptyPathDir = mkdtempSync(join(tmpdir(), 'goosar-empty-path-'));
    const originalPath = process.env.PATH;
    process.env.PATH = emptyPathDir;
    try {
      await syncProvisionedPackages({ ctx: ctx(), fetchImpl });
    } finally {
      process.env.PATH = originalPath;
      rmSync(emptyPathDir, { recursive: true, force: true });
    }

    const installDir = packageInstallDir(ctx(), pkg);
    expect(readFileSync(join(installDir, 'SKILL.md'), 'utf-8')).toBe('# office\n');
  });
});

describe('provisioning restart hook (blocker #189-2a: stale daemon PATH after install)', () => {
  it('fires the restart hook after a sync pass that installs a NEW package', async () => {
    setProvisioningCredentials('https://goosar.example', 'gsl_test');
    setProvisioningWorkspaceId('ws_1');
    const tar = buildTar([{ name: 'SKILL.md', type: 'file', data: Buffer.from('v1') }]);
    const pkg = manifestPackage({ sha256: sha256Hex(tar) });
    const fetchImpl: AuthedFetch = async (input) => {
      if (String(input).includes('/manifest')) {
        return new Response(JSON.stringify({ schemaVersion: 1, packages: [pkg] }), { status: 200 });
      }
      return new Response(new Uint8Array(tar), { status: 200 });
    };

    const hook = vi.fn();
    setProvisioningRestartHook(hook);

    await syncProvisionedPackages({ ctx: ctx(), fetchImpl, decompress: async (i) => i });

    expect(hook).toHaveBeenCalledTimes(1);
  });

  it('does NOT fire the restart hook when every package is already in sync (nothing installed this pass)', async () => {
    setProvisioningCredentials('https://goosar.example', 'gsl_test');
    setProvisioningWorkspaceId('ws_1');
    const tar = buildTar([{ name: 'SKILL.md', type: 'file', data: Buffer.from('v1') }]);
    const pkg = manifestPackage({ sha256: sha256Hex(tar) });
    const fetchImpl: AuthedFetch = async (input) => {
      if (String(input).includes('/manifest')) {
        return new Response(JSON.stringify({ schemaVersion: 1, packages: [pkg] }), { status: 200 });
      }
      return new Response(new Uint8Array(tar), { status: 200 });
    };

    await syncProvisionedPackages({ ctx: ctx(), fetchImpl, decompress: async (i) => i });

    const hook = vi.fn();
    setProvisioningRestartHook(hook);

    await syncProvisionedPackages({ ctx: ctx(), fetchImpl, decompress: async (i) => i });

    expect(hook).not.toHaveBeenCalled();
  });
});

describe('provisioned runtime `current` symlink indirection (blocker #189-2b)', () => {
  const auth = { apiBaseUrl: 'https://goosar.example', token: 'gsl_test', workspaceId: 'ws_1' };

  function runtimePkg(overrides: Partial<PackageManifest> = {}): PackageManifest {
    const tar = buildTar([
      { name: 'bin/chromium', type: 'file', data: Buffer.from('binary-payload') },
    ]);
    return {
      schemaVersion: 1,
      name: 'playwright-browsers',
      version: '1.0.0',
      type: 'runtime',
      platform: 'darwin-arm64',
      sha256: sha256Hex(tar),
      size: tar.length,
      requires: [],
      ...overrides,
    };
  }

  it('resolveProvisionedRuntimeDir returns null when nothing is provisioned yet', () => {
    expect(resolveProvisionedRuntimeDir(ctx(), 'playwright-browsers')).toBeNull();
  });

  it('a fresh install switches `current` to the newly installed version, atomically', async () => {
    const tar = buildTar([
      { name: 'bin/chromium', type: 'file', data: Buffer.from('binary-payload') },
    ]);
    const pkg = runtimePkg({ sha256: sha256Hex(tar) });
    const fetchImpl: AuthedFetch = async () => new Response(new Uint8Array(tar), { status: 200 });

    const state = await syncOnePackage(ctx(), pkg, auth, {
      fetchImpl,
      decompress: async (i) => i,
    });
    expect(state).toBe('ok');

    const root = runtimePackageRoot(ctx(), 'playwright-browsers');
    const currentPath = join(root, 'current');
    expect(lstatSync(currentPath).isSymbolicLink()).toBe(true);
    expect(readlinkSync(currentPath)).toBe('playwright-browsers@1.0.0');

    const resolved = resolveProvisionedRuntimeDir(ctx(), 'playwright-browsers');
    expect(resolved).toBe(join(root, 'playwright-browsers@1.0.0'));
    expect(readFileSync(join(resolved as string, 'bin', 'chromium'), 'utf-8')).toBe(
      'binary-payload',
    );
  });

  it('upgrading to a new version atomically re-points `current` at the new version dir', async () => {
    const tarV1 = buildTar([{ name: 'bin/chromium', type: 'file', data: Buffer.from('v1') }]);
    const pkgV1 = runtimePkg({ version: '1.0.0', sha256: sha256Hex(tarV1) });
    await syncOnePackage(ctx(), pkgV1, auth, {
      fetchImpl: async () => new Response(new Uint8Array(tarV1), { status: 200 }),
      decompress: async (i) => i,
    });

    const tarV2 = buildTar([{ name: 'bin/chromium', type: 'file', data: Buffer.from('v2') }]);
    const pkgV2 = runtimePkg({ version: '2.0.0', sha256: sha256Hex(tarV2) });
    const state = await syncOnePackage(ctx(), pkgV2, auth, {
      fetchImpl: async () => new Response(new Uint8Array(tarV2), { status: 200 }),
      decompress: async (i) => i,
    });
    expect(state).toBe('ok');

    const root = runtimePackageRoot(ctx(), 'playwright-browsers');
    expect(readlinkSync(join(root, 'current'))).toBe('playwright-browsers@2.0.0');
    const resolved = resolveProvisionedRuntimeDir(ctx(), 'playwright-browsers');
    expect(readFileSync(join(resolved as string, 'bin', 'chromium'), 'utf-8')).toBe('v2');
  });

  it('retries a current-switch that a previous sync left undone, without re-downloading', async () => {
    const tar = buildTar([{ name: 'bin/chromium', type: 'file', data: Buffer.from('v1') }]);
    const pkg = runtimePkg({ sha256: sha256Hex(tar) });
    const fetchImpl = vi.fn<AuthedFetch>(
      async () => new Response(new Uint8Array(tar), { status: 200 }),
    );

    await syncOnePackage(ctx(), pkg, auth, { fetchImpl, decompress: async (i) => i });
    expect(fetchImpl).toHaveBeenCalledTimes(1);

    const root = runtimePackageRoot(ctx(), 'playwright-browsers');
    rmSync(join(root, 'current'), { force: true });
    expect(resolveProvisionedRuntimeDir(ctx(), 'playwright-browsers')).toBeNull();

    const state = await syncOnePackage(ctx(), pkg, auth, {
      fetchImpl,
      decompress: async (i) => i,
    });

    expect(state).toBe('ok');
    expect(fetchImpl).toHaveBeenCalledTimes(1);
    expect(readlinkSync(join(root, 'current'))).toBe('playwright-browsers@1.0.0');
  });

  it('an interrupted current-switch never leaves `current` dangling — it stays exactly as it was before the attempt', async () => {
    const tarV1 = buildTar([{ name: 'bin/chromium', type: 'file', data: Buffer.from('v1') }]);
    const pkgV1 = runtimePkg({ version: '1.0.0', sha256: sha256Hex(tarV1) });
    await syncOnePackage(ctx(), pkgV1, auth, {
      fetchImpl: async () => new Response(new Uint8Array(tarV1), { status: 200 }),
      decompress: async (i) => i,
    });

    const root = runtimePackageRoot(ctx(), 'playwright-browsers');
    const currentPath = join(root, 'current');
    expect(readlinkSync(currentPath)).toBe('playwright-browsers@1.0.0');

    rmSync(currentPath, { recursive: true, force: true });
    mkdirSync(currentPath);
    writeFileSync(join(currentPath, 'keep.txt'), 'must survive');

    const tarV2 = buildTar([{ name: 'bin/chromium', type: 'file', data: Buffer.from('v2') }]);
    const pkgV2 = runtimePkg({ version: '2.0.0', sha256: sha256Hex(tarV2) });
    const state = await syncOnePackage(ctx(), pkgV2, auth, {
      fetchImpl: async () => new Response(new Uint8Array(tarV2), { status: 200 }),
      decompress: async (i) => i,
    });

    expect(state).toBe('fail');
    expect(lstatSync(currentPath).isSymbolicLink()).toBe(false);
    expect(readFileSync(join(currentPath, 'keep.txt'), 'utf-8')).toBe('must survive');
    const leftovers = readdirSync(root).filter((name) => name.startsWith('.current.tmp-'));
    expect(leftovers).toEqual([]);
  });
});

describe('runtime old-version pruning (T-110, issue #234)', () => {
  const auth = {
    apiBaseUrl: 'https://goosar.example',
    token: 'gsl_test',
    workspaceId: 'ws_1',
  };

  async function installRuntimeVersion(version: string, payload: string) {
    const tar = buildTar([{ name: 'bin/chromium', type: 'file', data: Buffer.from(payload) }]);
    const pkg: PackageManifest = {
      schemaVersion: 1,
      name: 'playwright-browsers',
      version,
      type: 'runtime',
      platform: 'darwin-arm64',
      sha256: sha256Hex(tar),
      size: tar.length,
      requires: [],
    };
    const state = await syncOnePackage(ctx(), pkg, auth, {
      fetchImpl: async () => new Response(new Uint8Array(tar), { status: 200 }),
      decompress: async (i) => i,
    });
    expect(state).toBe('ok');
  }

  function versionDirs(): string[] {
    const root = runtimePackageRoot(ctx(), 'playwright-browsers');
    return readdirSync(root, { withFileTypes: true })
      .filter((e) => e.isDirectory() && e.name.startsWith('playwright-browsers@'))
      .map((e) => e.name)
      .sort();
  }

  it('a single upgrade keeps the previous version on disk as the rollback margin', async () => {
    await installRuntimeVersion('1.0.0', 'v1');
    await installRuntimeVersion('2.0.0', 'v2');

    expect(versionDirs()).toEqual(['playwright-browsers@1.0.0', 'playwright-browsers@2.0.0']);
    const root = runtimePackageRoot(ctx(), 'playwright-browsers');
    expect(readlinkSync(join(root, 'current'))).toBe('playwright-browsers@2.0.0');
  });

  it('after a second upgrade at most two versions remain: active + immediately previous', async () => {
    await installRuntimeVersion('1.0.0', 'v1');
    await installRuntimeVersion('2.0.0', 'v2');
    await installRuntimeVersion('3.0.0', 'v3');

    expect(versionDirs()).toEqual(['playwright-browsers@2.0.0', 'playwright-browsers@3.0.0']);
    const root = runtimePackageRoot(ctx(), 'playwright-browsers');
    expect(readlinkSync(join(root, 'current'))).toBe('playwright-browsers@3.0.0');
    const previous = join(root, 'playwright-browsers@2.0.0');
    expect(readFileSync(join(previous, 'bin', 'chromium'), 'utf-8')).toBe('v2');
  });

  it('pruning ignores the current symlink, dot-temp files and unrelated entries', async () => {
    await installRuntimeVersion('1.0.0', 'v1');
    const root = runtimePackageRoot(ctx(), 'playwright-browsers');
    writeFileSync(join(root, '.playwright-browsers@9.9.9-abcdef123456.download.tmp'), 'partial');
    writeFileSync(join(root, 'NOTES.txt'), 'user note');

    await installRuntimeVersion('2.0.0', 'v2');
    await installRuntimeVersion('3.0.0', 'v3');

    expect(
      readFileSync(join(root, '.playwright-browsers@9.9.9-abcdef123456.download.tmp'), 'utf-8'),
    ).toBe('partial');
    expect(readFileSync(join(root, 'NOTES.txt'), 'utf-8')).toBe('user note');
    expect(versionDirs()).toEqual(['playwright-browsers@2.0.0', 'playwright-browsers@3.0.0']);
  });
});

function resolveGnuTarPath(): string | null {
  try {
    const resolved = execFileSync('which', ['gtar'], { encoding: 'utf-8' }).trim();
    return resolved.length > 0 ? resolved : null;
  } catch {
    return null;
  }
}

function packWithSystemTar(srcDir: string, outDir: string): { tarZstPath: string } {
  const files = readdirSync(srcDir, { recursive: true, encoding: 'utf-8' })
    .filter((rel) => !lstatSync(join(srcDir, rel)).isDirectory())
    .sort();
  const tarPath = join(outDir, 'fixture.tar');
  const listPath = join(outDir, 'fixture.filelist');
  writeFileSync(listPath, `${files.join('\n')}\n`);
  const isGnu = /GNU tar/i.test(execFileSync('tar', ['--version'], { encoding: 'utf-8' }));
  execFileSync('tar', [
    '--format',
    isGnu ? 'posix' : 'gnutar',
    '-cf',
    tarPath,
    '-C',
    srcDir,
    '-T',
    listPath,
  ]);
  const tarZstPath = join(outDir, 'fixture.tar.zst');
  writeFileSync(tarZstPath, zstdCompressSync(readFileSync(tarPath)));
  return { tarZstPath };
}

const gnuTarPath = resolveGnuTarPath();
if (!gnuTarPath) {
  console.warn(
    '[provisioning.test.ts] GNU tar (gtar) not found on this host — skipping the ' +
      'forced GNU/PAX branch of the finding-10 end-to-end gate (install e.g. via ' +
      "`brew install gnu-tar` to cover it locally; CI's Linux runners ship GNU tar " +
      'as the default `tar`).',
  );
}

describe('end-to-end: system tar -> real .tar.zst -> reader/extractor round trip', () => {
  function makeLongPathFixture(rootDir: string, name: string): string {
    writeFileSync(
      join(rootDir, 'provisioning-package.json'),
      JSON.stringify({ name, version: '1.0.0', type: 'mcp-server' }),
    );
    const segs = Array.from({ length: 8 }, (_, i) => `dependency-name-segment-${i + 1}`);
    const deep = join(rootDir, ...segs);
    mkdirSync(deep, { recursive: true });
    writeFileSync(join(deep, 'module.py'), "print('hi')\n");
    symlinkSync(join('..', 'module.py'), join(deep, 'module-link.py'));

    const relPathLength = join(...segs, 'module.py').length;
    if (relPathLength <= 100) {
      throw new Error(
        `fixture path too short to exercise long-name handling: ${relPathLength} chars`,
      );
    }
    return deep;
  }

  async function extractTarZst(
    tarZstPath: string,
  ): Promise<{ ok: boolean; dest: string; reason?: string }> {
    const compressed = readFileSync(tarZstPath);
    const tarBuffer = await systemZstdDecompress(compressed);
    const dest = mkdtempSync(join(home, 'e2e-extract-'));
    const outcome = await extractPackageTar(tarBuffer, dest);
    return outcome.ok ? { ok: true, dest } : { ok: false, dest, reason: outcome.reason };
  }

  it("packs a long-path + internal-symlink fixture with the system tar (bsdtar/gnutar on this host, GNU 'L'/'K' records) and the reader round-trips it tree-identical", async () => {
    const srcDir = mkdtempSync(join(home, 'e2e-src-ustar-'));
    makeLongPathFixture(srcDir, 'office-suite-ustar');
    const outDir = mkdtempSync(join(home, 'e2e-out-ustar-'));

    const packed = packWithSystemTar(srcDir, outDir);
    const { ok, dest, reason } = await extractTarZst(packed.tarZstPath);

    expect(reason).toBeUndefined();
    expect(ok).toBe(true);
    expect(await hashArtifactTree(dest)).toBe(await hashArtifactTree(srcDir));
  });

  (gnuTarPath ? it : it.skip)(
    'packs the SAME shape of fixture forcing the GNU/PAX branch (gtar) and the reader round-trips it — the finding-3 gate',
    async () => {
      const shimDir = mkdtempSync(join(home, 'e2e-tar-shim-'));
      symlinkSync(gnuTarPath as string, join(shimDir, 'tar'));
      const originalPath = process.env.PATH;
      process.env.PATH = `${shimDir}:${originalPath ?? ''}`;
      try {
        const srcDir = mkdtempSync(join(home, 'e2e-src-gnu-'));
        makeLongPathFixture(srcDir, 'office-suite-gnu');
        const outDir = mkdtempSync(join(home, 'e2e-out-gnu-'));

        const packed = packWithSystemTar(srcDir, outDir);
        const { ok, dest, reason } = await extractTarZst(packed.tarZstPath);

        expect(reason).toBeUndefined();
        expect(ok).toBe(true);
        expect(await hashArtifactTree(dest)).toBe(await hashArtifactTree(srcDir));
      } finally {
        process.env.PATH = originalPath;
      }
    },
  );
});

describe('parseRevokedPackages (issue #242)', () => {
  it('parses type:name@version entries and tolerates a missing version', () => {
    expect(parseRevokedPackageRef('mcp-server:outlook@1.2.0')).toEqual({
      type: 'mcp-server',
      name: 'outlook',
      version: '1.2.0',
    });
    expect(parseRevokedPackageRef('skill:office-docx')).toEqual({
      type: 'skill',
      name: 'office-docx',
    });
  });

  it('drops malformed and hostile entries — nothing path-shaped may reach the removal code', () => {
    for (const bad of [
      42,
      null,
      '',
      'no-colon',
      'unknown-type:name@1',
      'skill:',
      'skill:../../etc@1.0.0',
      'skill:..',
      'skill:with/slash@1.0.0',
      'skill:with\\backslash',
      'mcp-server:.hidden@1.0.0',
      'skill:ok@../traversal',
    ]) {
      expect(parseRevokedPackageRef(bad)).toBeNull();
    }
  });

  it('parses the manifest response field, dropping bad entries per-entry', () => {
    const refs = parseRevokedPackages({
      schemaVersion: 1,
      packages: [],
      revokedPackages: ['skill:good@1.0.0', 'skill:../evil', 7],
    });
    expect(refs).toEqual([{ type: 'skill', name: 'good', version: '1.0.0' }]);
  });
});

describe('parseUnavailablePackages (T-10, #694)', () => {
  it('parses the manifest response field, dropping malformed entries', () => {
    const refs = parseUnavailablePackages({
      schemaVersion: 1,
      packages: [],
      unavailablePackages: [
        {
          key: 'skill:mail-triage@1.0.0',
          reason: 'dependency unavailable: mail-triage requires ews-mcp@0.1.0',
        },
        { key: 'bad-entry' }, // missing reason
        { key: 42, reason: 'not a string key' },
        'not even an object',
        null,
      ],
    });
    expect(refs).toEqual([
      {
        key: 'skill:mail-triage@1.0.0',
        reason: 'dependency unavailable: mail-triage requires ews-mcp@0.1.0',
      },
    ]);
  });

  it('is malformed-response tolerant — a missing/malformed field never throws', () => {
    expect(parseUnavailablePackages(null)).toEqual([]);
    expect(parseUnavailablePackages({ packages: [] })).toEqual([]);
    expect(parseUnavailablePackages({ packages: [], unavailablePackages: 'not-an-array' })).toEqual(
      [],
    );
  });
});

describe('unavailablePackageName', () => {
  it('extracts the package name out of a type:name@version key', () => {
    expect(unavailablePackageName('mcp-server:ews-mcp@0.1.0')).toBe('ews-mcp');
    expect(unavailablePackageName('skill:mail-triage@1.0.0')).toBe('mail-triage');
  });

  it('degrades gracefully for a key with no version or no type prefix', () => {
    expect(unavailablePackageName('skill:solo')).toBe('solo');
    expect(unavailablePackageName('just-a-name')).toBe('just-a-name');
  });
});

describe('removeOneRevokedPackage (issue #242)', () => {
  function installMarked(type: 'skill' | 'mcp-server', name: string): string {
    const dir = join(packageFamilyRoot(ctx(), type), name);
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, 'payload.txt'), 'bytes');
    writeFileSync(
      join(dir, '.provisioned.json'),
      JSON.stringify({
        name,
        version: '1.0.0',
        platform: 'darwin-arm64',
        sha256: 'a'.repeat(64),
        installedAt: '2026-01-01T00:00:00.000Z',
      }),
    );
    return dir;
  }

  it('removes the marked directory and leaves the unmarked neighbor untouched (contract #190)', async () => {
    const marked = installMarked('mcp-server', 'outlook');
    const neighbor = join(packageFamilyRoot(ctx(), 'mcp-server'), 'hand-rolled');
    mkdirSync(neighbor, { recursive: true });
    writeFileSync(join(neighbor, 'precious.txt'), 'do not delete');

    const outcome = await removeOneRevokedPackage(ctx(), {
      type: 'mcp-server',
      name: 'outlook',
    });

    expect(outcome).toBe('removed');
    expect(existsSync(marked)).toBe(false);
    expect(existsSync(join(neighbor, 'precious.txt'))).toBe(true);
  });

  it('never deletes an UNMARKED directory the server names — contract #190 beats the server', async () => {
    const dir = join(packageFamilyRoot(ctx(), 'skill'), 'office-docx');
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, 'edited-by-hand.md'), 'user content');

    const outcome = await removeOneRevokedPackage(ctx(), {
      type: 'skill',
      name: 'office-docx',
    });

    expect(outcome).toBe('unmarked-kept');
    expect(existsSync(join(dir, 'edited-by-hand.md'))).toBe(true);
  });

  it('defers while the daemon is busy (contract #189) and removes once idle', async () => {
    const dir = installMarked('skill', 'office-docx');

    expect(
      await removeOneRevokedPackage(
        ctx(),
        { type: 'skill', name: 'office-docx' },
        async () => true,
      ),
    ).toBe('deferred');
    expect(existsSync(dir)).toBe(true);

    expect(
      await removeOneRevokedPackage(
        ctx(),
        { type: 'skill', name: 'office-docx' },
        async () => false,
      ),
    ).toBe('removed');
    expect(existsSync(dir)).toBe(false);
  });

  it('reports absent for a package that was never installed', async () => {
    expect(await removeOneRevokedPackage(ctx(), { type: 'skill', name: 'never-here' })).toBe(
      'absent',
    );
  });

  it('runtime: removes marked version dirs, clears a dangling current, keeps unmarked versions', async () => {
    const root = runtimePackageRoot(ctx(), 'playwright-browsers');
    const markedDir = join(root, 'playwright-browsers@2.0.0');
    mkdirSync(markedDir, { recursive: true });
    writeFileSync(join(markedDir, 'chromium'), 'bin');
    writeFileSync(
      join(markedDir, '.provisioned.json'),
      JSON.stringify({
        name: 'playwright-browsers',
        version: '2.0.0',
        platform: 'darwin-arm64',
        sha256: 'b'.repeat(64),
        installedAt: '2026-01-01T00:00:00.000Z',
      }),
    );
    const unmarkedDir = join(root, 'playwright-browsers@0.9.0-manual');
    mkdirSync(unmarkedDir, { recursive: true });
    writeFileSync(join(unmarkedDir, 'keep.txt'), 'manual');
    symlinkSync('playwright-browsers@2.0.0', join(root, 'current'));

    const outcome = await removeOneRevokedPackage(ctx(), {
      type: 'runtime',
      name: 'playwright-browsers',
    });

    expect(outcome).toBe('removed');
    expect(existsSync(markedDir)).toBe(false);
    expect(existsSync(join(unmarkedDir, 'keep.txt'))).toBe(true);
    expect(resolveProvisionedRuntimeDir(ctx(), 'playwright-browsers')).toBeNull();
  });
});

describe('runSync: revoked packages end to end (issue #242)', () => {
  function markedInstall(type: 'skill' | 'mcp-server', name: string): string {
    const dir = join(packageFamilyRoot(ctx(), type), name);
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, 'payload.txt'), 'bytes');
    writeFileSync(
      join(dir, '.provisioned.json'),
      JSON.stringify({
        name,
        version: '1.0.0',
        platform: 'darwin-arm64',
        sha256: 'c'.repeat(64),
        installedAt: '2026-01-01T00:00:00.000Z',
      }),
    );
    return dir;
  }

  function bindAuth(): void {
    setProvisioningCredentials('https://goosar.example', 'gsl_test');
    setProvisioningWorkspaceId('ws_1');
  }

  it('a revoked package is removed, reported in removedPackages, and the restart hook fires', async () => {
    bindAuth();
    const revokedDir = markedInstall('mcp-server', 'outlook');
    const { pkg, tar } = pkgWithBlob({ name: 'office-docx', type: 'skill' });
    const restartHook = vi.fn();
    setProvisioningRestartHook(restartHook);

    const fetchImpl: AuthedFetch = async (input) => {
      const url = String(input);
      if (url.includes('/manifest')) {
        return new Response(
          JSON.stringify({
            schemaVersion: 1,
            packages: [pkg],
            revokedPackages: ['mcp-server:outlook@1.0.0'],
          }),
          { status: 200 },
        );
      }
      return new Response(new Uint8Array(tar), { status: 200 });
    };

    await syncProvisionedPackages({ ctx: ctx(), fetchImpl, decompress: async (i) => i });

    expect(existsSync(revokedDir)).toBe(false);
    const status = getProvisioningStatus();
    expect(status.removedPackages).toEqual([
      { name: 'outlook', type: 'mcp-server', version: '1.0.0', state: 'removed' },
    ]);
    expect(restartHook).toHaveBeenCalled();
  });

  it('an EMPTY manifest still processes revocations — a workspace that provisions nothing anymore disarms, not resurrects', async () => {
    bindAuth();
    const revokedDir = markedInstall('skill', 'office-docx');

    const fetchImpl: AuthedFetch = async () =>
      new Response(
        JSON.stringify({
          schemaVersion: 1,
          packages: [],
          revokedPackages: ['skill:office-docx@1.0.0'],
        }),
        { status: 200 },
      );

    await syncProvisionedPackages({ ctx: ctx(), fetchImpl, decompress: async (i) => i });

    expect(existsSync(revokedDir)).toBe(false);
    const status = getProvisioningStatus();
    expect(status.configured).toBe(false); 
    expect(status.removedPackages).toEqual([
      { name: 'office-docx', type: 'skill', version: '1.0.0', state: 'removed' },
    ]);
  });

  it('a busy daemon defers the removal (pending), a later idle pass completes it', async () => {
    bindAuth();
    const revokedDir = markedInstall('mcp-server', 'outlook');
    const fetchImpl: AuthedFetch = async () =>
      new Response(
        JSON.stringify({
          schemaVersion: 1,
          packages: [],
          revokedPackages: ['mcp-server:outlook@1.0.0'],
        }),
        { status: 200 },
      );

    await retryProvisioning({ ctx: ctx(), fetchImpl, isDaemonBusy: async () => true });
    expect(existsSync(revokedDir)).toBe(true); 
    expect(getProvisioningStatus().removedPackages).toEqual([
      { name: 'outlook', type: 'mcp-server', version: '1.0.0', state: 'pending' },
    ]);

    await retryProvisioning({ ctx: ctx(), fetchImpl, isDaemonBusy: async () => false });
    expect(existsSync(revokedDir)).toBe(false);
    expect(getProvisioningStatus().removedPackages).toEqual([
      { name: 'outlook', type: 'mcp-server', version: '1.0.0', state: 'removed' },
    ]);
  });

  it('re-delivery after a revocation reinstalls and clears the removal record (no eternal ban)', async () => {
    bindAuth();
    markedInstall('skill', 'office-docx');

    const revokingFetch: AuthedFetch = async () =>
      new Response(
        JSON.stringify({
          schemaVersion: 1,
          packages: [],
          revokedPackages: ['skill:office-docx@1.0.0'],
        }),
        { status: 200 },
      );
    await retryProvisioning({ ctx: ctx(), fetchImpl: revokingFetch, decompress: async (i) => i });
    expect(getProvisioningStatus().removedPackages).toHaveLength(1);

    const { pkg, tar } = pkgWithBlob({ name: 'office-docx', type: 'skill' });
    const servingFetch = manifestAndBlobFetch([pkg], new Map([['office-docx@1.0.0', tar]]));
    await retryProvisioning({ ctx: ctx(), fetchImpl: servingFetch, decompress: async (i) => i });

    expect(existsSync(join(packageFamilyRoot(ctx(), 'skill'), 'office-docx', 'file.txt'))).toBe(
      true,
    );
    expect(getProvisioningStatus().removedPackages).toEqual([]);
  });

  it('never removes a package the same response still serves (server-bug safety net)', async () => {
    bindAuth();
    const { pkg, tar } = pkgWithBlob({ name: 'outlook', type: 'mcp-server' });
    const fetchImpl: AuthedFetch = async (input) => {
      const url = String(input);
      if (url.includes('/manifest')) {
        return new Response(
          JSON.stringify({
            schemaVersion: 1,
            packages: [pkg],
            revokedPackages: ['mcp-server:outlook@1.0.0'],
          }),
          { status: 200 },
        );
      }
      return new Response(new Uint8Array(tar), { status: 200 });
    };

    await syncProvisionedPackages({ ctx: ctx(), fetchImpl, decompress: async (i) => i });

    expect(existsSync(join(packageFamilyRoot(ctx(), 'mcp-server'), 'outlook', 'file.txt'))).toBe(
      true,
    );
    expect(getProvisioningStatus().removedPackages ?? []).toEqual([]);
  });
});

describe('runSync: access revoked — excluded member disarms (issue #242, T-060)', () => {
  function bindAuth(): void {
    setProvisioningCredentials('https://goosar.example', 'gsl_test');
    setProvisioningWorkspaceId('ws_1');
  }

  function markedInstall(type: 'skill' | 'mcp-server', name: string): string {
    const dir = join(packageFamilyRoot(ctx(), type), name);
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, 'payload.txt'), 'bytes');
    writeFileSync(
      join(dir, '.provisioned.json'),
      JSON.stringify({
        name,
        version: '1.0.0',
        platform: 'darwin-arm64',
        sha256: 'e'.repeat(64),
        installedAt: '2026-01-01T00:00:00.000Z',
      }),
    );
    return dir;
  }

  const denialFetch: AuthedFetch = async () =>
    new Response(JSON.stringify({ error: 'workspace not found' }), { status: 404 });

  async function confirmDenial(base: number): Promise<void> {
    await syncProvisionedPackages({ ctx: ctx(), fetchImpl: denialFetch, now: () => base });
    await syncProvisionedPackages({
      ctx: ctx(),
      fetchImpl: denialFetch,
      now: () => base + 5 * 60_000,
    });
    await syncProvisionedPackages({
      ctx: ctx(),
      fetchImpl: denialFetch,
      now: () => base + 11 * 60_000,
    });
  }

  it('a CONFIRMED denial (3 consecutive JSON 404s over 10+ min) wipes every MARKED package and keeps unmarked content', async () => {
    bindAuth();
    const markedSkill = markedInstall('skill', 'office-docx');
    const markedMcp = markedInstall('mcp-server', 'outlook');
    const unmarked = join(packageFamilyRoot(ctx(), 'skill'), 'hand-rolled');
    mkdirSync(unmarked, { recursive: true });
    writeFileSync(join(unmarked, 'precious.txt'), 'keep');

    await confirmDenial(1_000_000);

    expect(existsSync(markedSkill)).toBe(false);
    expect(existsSync(markedMcp)).toBe(false);
    expect(existsSync(join(unmarked, 'precious.txt'))).toBe(true);

    const status = getProvisioningStatus();
    expect(status.state).toBe('fail');
    expect(status.reasonCode).toBe('provisioning_access_revoked');
    expect((status.removedPackages ?? []).map((p) => `${p.type}:${p.name}`).sort()).toEqual([
      'mcp-server:outlook',
      'skill:office-docx',
    ]);
  });

  it('a SINGLE definitive JSON 404 does not wipe (pass-2 H2: one answer is not proof)', async () => {
    bindAuth();
    const marked = markedInstall('skill', 'office-docx');

    await syncProvisionedPackages({ ctx: ctx(), fetchImpl: denialFetch });

    expect(existsSync(marked)).toBe(true);
    const status = getProvisioningStatus();
    expect(status.state).toBe('fail');
    expect(status.reasonCode).toBe('provisioning_access_revoked');
    expect(status.removedPackages ?? []).toEqual([]);
  });

  it('three denials inside one bad minute do not wipe (window unmet)', async () => {
    bindAuth();
    const marked = markedInstall('skill', 'office-docx');

    const base = 2_000_000;
    for (let i = 0; i < 4; i += 1) {
      await syncProvisionedPackages({
        ctx: ctx(),
        fetchImpl: denialFetch,
        now: () => base + i * 1_000,
      });
    }

    expect(existsSync(marked)).toBe(true);
  });

  it('any other answer between denials resets the run', async () => {
    bindAuth();
    const marked = markedInstall('skill', 'office-docx');
    const { pkg, tar } = pkgWithBlob({ name: 'office-docx', type: 'skill' });
    const servingFetch = manifestAndBlobFetch([pkg], new Map([['office-docx@1.0.0', tar]]));

    const base = 3_000_000;
    await syncProvisionedPackages({ ctx: ctx(), fetchImpl: denialFetch, now: () => base });
    await syncProvisionedPackages({
      ctx: ctx(),
      fetchImpl: denialFetch,
      now: () => base + 6 * 60_000,
    });
    await syncProvisionedPackages({
      ctx: ctx(),
      fetchImpl: servingFetch,
      decompress: async (i) => i,
      now: () => base + 8 * 60_000,
    });
    await syncProvisionedPackages({
      ctx: ctx(),
      fetchImpl: denialFetch,
      now: () => base + 12 * 60_000,
    });
    await syncProvisionedPackages({
      ctx: ctx(),
      fetchImpl: denialFetch,
      now: () => base + 25 * 60_000,
    });

    expect(existsSync(marked)).toBe(true);
  });

  it("a 503 with Retry-After (membership lookup outage) is an ERROR that keeps state, never 'unconfigured'", async () => {
    bindAuth();
    const marked = markedInstall('skill', 'office-docx');

    const fetchImpl: AuthedFetch = async () =>
      new Response(JSON.stringify({ error: 'workspace lookup temporarily unavailable' }), {
        status: 503,
        headers: { 'Retry-After': '30' },
      });

    await syncProvisionedPackages({ ctx: ctx(), fetchImpl });

    expect(existsSync(marked)).toBe(true);
    const status = getProvisioningStatus();
    expect(status.configured).toBe(true);
    expect(status.state).toBe('fail');
    expect(status.reasonCode).toBe('provisioning_manifest_unreachable');
  });

  it('a plain-text 404 (proxy / wrong base URL) is an ERROR, not a wipe', async () => {
    bindAuth();
    const marked = markedInstall('skill', 'office-docx');

    const fetchImpl: AuthedFetch = async () => new Response('404 page not found', { status: 404 });

    await syncProvisionedPackages({ ctx: ctx(), fetchImpl });

    expect(existsSync(marked)).toBe(true);
    expect(getProvisioningStatus().state).toBe('fail');
    expect(getProvisioningStatus().reasonCode).toBe('provisioning_manifest_unreachable');
  });

  it('a transient network error removes nothing', async () => {
    bindAuth();
    const marked = markedInstall('mcp-server', 'outlook');

    const fetchImpl: AuthedFetch = async () => {
      throw new Error('ECONNREFUSED');
    };

    await syncProvisionedPackages({ ctx: ctx(), fetchImpl });

    expect(existsSync(marked)).toBe(true);
  });

  it("resets the strike series when the workspace or credentials change (pass-2 M): workspace A's denials never confirm a wipe under workspace B", async () => {
    bindAuth();
    const dir = markedInstall('skill', 'strike-scope');
    const base = 1_700_000_000_000;

    await syncProvisionedPackages({ ctx: ctx(), fetchImpl: denialFetch, now: () => base });
    await syncProvisionedPackages({
      ctx: ctx(),
      fetchImpl: denialFetch,
      now: () => base + 6 * 60_000,
    });

    setProvisioningWorkspaceId('ws_2');
    setProvisioningWorkspaceId('ws_1');

    await syncProvisionedPackages({
      ctx: ctx(),
      fetchImpl: denialFetch,
      now: () => base + 12 * 60_000,
    });
    expect(existsSync(dir)).toBe(true);
  });

  it('runWorkspaceAccessRevokedCleanup (member:removed, pass-2 L) wipes marked packages immediately — server-confirmed push needs no N-threshold', async () => {
    bindAuth();
    const marked = markedInstall('skill', 'office-docx');
    const unmarked = join(packageFamilyRoot(ctx(), 'skill'), 'hand-rolled');
    mkdirSync(unmarked, { recursive: true });
    writeFileSync(join(unmarked, 'precious.txt'), 'keep');

    await runWorkspaceAccessRevokedCleanup('ws_1', { ctx: ctx() });

    expect(existsSync(marked)).toBe(false);
    expect(existsSync(join(unmarked, 'precious.txt'))).toBe(true);
    const status = getProvisioningStatus();
    expect(status.reasonCode).toBe('provisioning_access_revoked');
    expect((status.removedPackages ?? []).map((p) => `${p.type}:${p.name}`)).toEqual([
      'skill:office-docx',
    ]);
  });

  it('runWorkspaceAccessRevokedCleanup ignores a loss in a workspace other than the bound one', async () => {
    bindAuth(); 
    const marked = markedInstall('skill', 'office-docx');

    await runWorkspaceAccessRevokedCleanup('ws_OTHER', { ctx: ctx() });
    await runWorkspaceAccessRevokedCleanup(null, { ctx: ctx() });

    expect(existsSync(marked)).toBe(true);
    expect(getProvisioningStatus().removedPackages ?? []).toEqual([]);
  });

  it('listInstalledProvisionedPackages sees only marked installs', async () => {
    markedInstall('skill', 'office-docx');
    const unmarked = join(packageFamilyRoot(ctx(), 'mcp-server'), 'hand-rolled');
    mkdirSync(unmarked, { recursive: true });
    const rtRoot = runtimePackageRoot(ctx(), 'playwright-browsers');
    const versionDir = join(rtRoot, 'playwright-browsers@2.0.0');
    mkdirSync(versionDir, { recursive: true });
    writeFileSync(
      join(versionDir, '.provisioned.json'),
      JSON.stringify({
        name: 'playwright-browsers',
        version: '2.0.0',
        platform: 'darwin-arm64',
        sha256: 'f'.repeat(64),
        installedAt: '2026-01-01T00:00:00.000Z',
      }),
    );

    const installed = await listInstalledProvisionedPackages(ctx());
    expect(installed.map((p) => `${p.type}:${p.name}`).sort()).toEqual([
      'runtime:playwright-browsers',
      'skill:office-docx',
    ]);
  });
});
