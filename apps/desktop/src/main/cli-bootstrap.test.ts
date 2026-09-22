import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createHash } from 'crypto';
import { existsSync, mkdtempSync, readFileSync, writeFileSync } from 'fs';
import { mkdirSync, rmSync } from 'fs';
import { join } from 'path';
import { tmpdir } from 'os';

const harness = vi.hoisted(() => ({
  userDataDir: '',
  execCalls: [] as string[][],
  extractedBinaryContent: 'binary-payload' as string | null,
}));

vi.mock('electron', () => ({
  app: {
    getPath: (name: string) => {
      if (name !== 'userData') throw new Error(`unexpected getPath: ${name}`);
      return harness.userDataDir;
    },
  },
}));

vi.mock('child_process', () => {
  const execFile = (
    cmd: string,
    args: string[],
    _opts: unknown,
    cb: (err: Error | null) => void,
  ) => {
    harness.execCalls.push([cmd, ...args]);
    if (cmd === 'tar' && harness.extractedBinaryContent !== null) {
      const dest = args[args.indexOf('-C') + 1];
      const bin = process.platform === 'win32' ? 'goosar.exe' : 'goosar';
      writeFileSync(join(dest, bin), harness.extractedBinaryContent);
    }
    cb(null);
  };
  return { execFile, default: { execFile } };
});

import { ensureManagedCli, managedCliPath } from './cli-bootstrap';

const OS = { darwin: 'darwin', linux: 'linux', win32: 'windows' }[
  process.platform as 'darwin' | 'linux' | 'win32'
];
const ARCH = { x64: 'amd64', arm64: 'arm64' }[process.arch as 'x64' | 'arm64'];
const EXT = process.platform === 'win32' ? 'zip' : 'tar.gz';
const ASSET = `goosar-cli-1.2.3-${OS}-${ARCH}.${EXT}`;

const ARCHIVE_BODY = 'archive-bytes';
const ARCHIVE_SHA = createHash('sha256').update(ARCHIVE_BODY).digest('hex');

function serveFetch(checksumsText: string, archiveBody: string = ARCHIVE_BODY) {
  const fetchMock = vi.fn(async (input: string | URL) => {
    const url = String(input);
    if (url.endsWith('/checksums.txt')) {
      return new Response(checksumsText, { status: 200 });
    }
    if (url.endsWith(`/${ASSET}`)) {
      return new Response(archiveBody, { status: 200 });
    }
    return new Response('not found', { status: 404, statusText: 'Not Found' });
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

beforeEach(() => {
  harness.userDataDir = mkdtempSync(join(tmpdir(), 'cli-bootstrap-test-'));
  harness.execCalls = [];
  harness.extractedBinaryContent = 'binary-payload';
  vi.stubEnv('GOOSAR_DESKTOP_NO_UPDATER', '');
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  rmSync(harness.userDataDir, { recursive: true, force: true });
});

describe('ensureManagedCli checksum verification', () => {
  it('installs the binary when the archive matches its checksum entry', async () => {
    serveFetch(`${ARCHIVE_SHA}  ${ASSET}\n`);

    const path = await ensureManagedCli({ forceInstall: true });

    expect(path).toBe(managedCliPath());
    expect(readFileSync(path, 'utf8')).toBe('binary-payload');
    expect(harness.execCalls.some(([cmd]) => cmd === 'tar')).toBe(true);
  });

  it("accepts BSD-style '*<filename>' checksum lines and uppercase hex", async () => {
    serveFetch(`${ARCHIVE_SHA.toUpperCase()}  *${ASSET}\n`);

    const path = await ensureManagedCli({ forceInstall: true });
    expect(existsSync(path)).toBe(true);
  });

  it('ignores malformed lines and still finds the valid entry', async () => {
    serveFetch(
      [
        '',
        'not-a-checksum-line',
        `deadbeef  too-short-hash.tar.gz`,
        `${'g'.repeat(64)}  non-hex-hash.tar.gz`,
        `${ARCHIVE_SHA}  ${ASSET}`,
        '   ',
      ].join('\n'),
    );

    const path = await ensureManagedCli({ forceInstall: true });
    expect(existsSync(path)).toBe(true);
  });

  it('throws on checksum mismatch and installs nothing', async () => {
    const wrongSha = createHash('sha256').update('other-bytes').digest('hex');
    serveFetch(`${wrongSha}  ${ASSET}\n`);

    await expect(ensureManagedCli({ forceInstall: true })).rejects.toThrow(/checksum mismatch/);
    expect(existsSync(managedCliPath())).toBe(false);
    expect(harness.execCalls.some(([cmd]) => cmd === 'tar')).toBe(false);
  });

  it('refuses to install when checksums.txt has no usable entry for the platform asset', async () => {
    serveFetch([`${ARCHIVE_SHA.slice(0, 40)}  ${ASSET}`, `${ARCHIVE_SHA}  other.txt`].join('\n'));

    await expect(ensureManagedCli({ forceInstall: true })).rejects.toThrow(
      /no release asset found/,
    );
    expect(existsSync(managedCliPath())).toBe(false);
    expect(harness.execCalls.length).toBe(0);
  });

  it('fails when checksums.txt itself cannot be fetched', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('nope', { status: 503, statusText: 'Service Unavailable' })),
    );

    await expect(ensureManagedCli({ forceInstall: true })).rejects.toThrow(
      /checksums\.txt fetch failed: 503/,
    );
  });

  it('fails when the verified archive did not contain the binary at its root', async () => {
    harness.extractedBinaryContent = null;
    serveFetch(`${ARCHIVE_SHA}  ${ASSET}\n`);

    await expect(ensureManagedCli({ forceInstall: true })).rejects.toThrow(/did not contain/);
    expect(existsSync(managedCliPath())).toBe(false);
  });
});

describe('ensureManagedCli gates', () => {
  it('returns the existing managed binary without touching the network', async () => {
    const fetchMock = serveFetch(`${ARCHIVE_SHA}  ${ASSET}\n`);
    mkdirSync(join(harness.userDataDir, 'bin'), { recursive: true });
    writeFileSync(managedCliPath(), 'already-installed');

    const path = await ensureManagedCli();

    expect(path).toBe(managedCliPath());
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('throws under the perimeter hard-off flag instead of downloading', async () => {
    const fetchMock = serveFetch(`${ARCHIVE_SHA}  ${ASSET}\n`);
    vi.stubEnv('GOOSAR_DESKTOP_NO_UPDATER', '1');

    await expect(ensureManagedCli()).rejects.toThrow(/managed CLI download is disabled/);
    expect(fetchMock).not.toHaveBeenCalled();
    expect(harness.execCalls.length).toBe(0);
  });

  it('still returns an already-installed binary when the hard-off flag is set', async () => {
    vi.stubEnv('GOOSAR_DESKTOP_NO_UPDATER', '1');
    mkdirSync(join(harness.userDataDir, 'bin'), { recursive: true });
    writeFileSync(managedCliPath(), 'already-installed');

    await expect(ensureManagedCli()).resolves.toBe(managedCliPath());
  });
});
