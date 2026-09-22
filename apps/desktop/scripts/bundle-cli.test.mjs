import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { spawnSync } from 'node:child_process';
import {
  cpSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  rmSync,
  writeFileSync,
  chmodSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
let root;

beforeEach(() => {
  root = mkdtempSync(join(tmpdir(), 'bundle-cli-'));
  mkdirSync(join(root, 'apps', 'desktop', 'scripts'), { recursive: true });
  mkdirSync(join(root, 'server'), { recursive: true });
  cpSync(join(here, 'bundle-cli.mjs'), join(root, 'apps', 'desktop', 'scripts', 'bundle-cli.mjs'));
});

afterEach(() => {
  rmSync(root, { recursive: true, force: true });
});

function run(env = {}) {
  return spawnSync(process.execPath, [join(root, 'apps', 'desktop', 'scripts', 'bundle-cli.mjs')], {
    encoding: 'utf-8',
    env: { PATH: '', HOME: root, ...env },
  });
}

const goos = { darwin: 'darwin', linux: 'linux', win32: 'windows' }[process.platform];
const goarch = process.arch === 'x64' ? 'amd64' : process.arch;

describe('bundle-cli.mjs without a Go toolchain', () => {
  it('dev build: skips quietly and leaves no resources/bin', () => {
    const r = run();
    expect(r.status).toBe(0);
    expect(existsSync(join(root, 'apps', 'desktop', 'resources', 'bin'))).toBe(false);
  });

  it('release build: a missing binary is a build failure, not a warning (#565)', () => {
    const r = run({ GOOSAR_RELEASE_BUILD: '1' });
    expect(r.status).toBe(1);
    expect(r.stderr).toMatch(/release build requires the bundled goosar CLI/);
    expect(existsSync(join(root, 'apps', 'desktop', 'resources', 'bin'))).toBe(false);
  });

  it('release build: a pre-built binary is bundled as before', () => {
    const binDir = join(root, 'server', 'bin', `${goos}-${goarch}`);
    mkdirSync(binDir, { recursive: true });
    const bin = join(binDir, process.platform === 'win32' ? 'goosar.exe' : 'goosar');
    writeFileSync(bin, '#!/bin/sh\necho goosar\n');
    chmodSync(bin, 0o755);
    const r = run({ GOOSAR_RELEASE_BUILD: '1' });
    expect(r.status).toBe(0);
    expect(existsSync(join(root, 'apps', 'desktop', 'resources', 'bin', 'goosar'))).toBe(true);
  });
});
