import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import {
  chmodSync,
  existsSync,
  lstatSync,
  mkdirSync,
  mkdtempSync,
  readlinkSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from 'fs';
import { join } from 'path';
import { tmpdir } from 'os';

import { describeCliPathResult, installCliOnPath } from './cli-path';

let root = '';
let linkDir = '';
let target = '';

beforeEach(() => {
  root = mkdtempSync(join(tmpdir(), 'cli-path-'));
  linkDir = join(root, 'bin');
  mkdirSync(linkDir);
  target = join(root, 'Goosar.app', 'resources', 'bin', 'goosar');
  mkdirSync(join(root, 'Goosar.app', 'resources', 'bin'), { recursive: true });
  writeFileSync(target, '#!/bin/sh\necho goosar\n');
  chmodSync(target, 0o755);
});

afterEach(() => {
  if (existsSync(linkDir)) chmodSync(linkDir, 0o755);
  rmSync(root, { recursive: true, force: true });
});

const unix = { platform: 'darwin' as NodeJS.Platform };

describe('installCliOnPath', () => {
  it('creates the symlink when nothing is there', async () => {
    const result = await installCliOnPath({ target, linkDir, ...unix });
    expect(result.state).toBe('installed');
    expect(readlinkSync(join(linkDir, 'goosar'))).toBe(target);
  });

  it('is idempotent: an existing link to the same target is left alone', async () => {
    await installCliOnPath({ target, linkDir, ...unix });
    const before = lstatSync(join(linkDir, 'goosar')).mtimeMs;
    const result = await installCliOnPath({ target, linkDir, ...unix });
    expect(result.state).toBe('already');
    expect(lstatSync(join(linkDir, 'goosar')).mtimeMs).toBe(before);
  });

  it('replaces a stale link left by an earlier version of the app', async () => {
    const old = join(root, 'old', 'Goosar.app', 'resources', 'bin', 'goosar');
    symlinkSync(old, join(linkDir, 'goosar')); 
    const result = await installCliOnPath({ target, linkDir, ...unix });
    expect(result.state).toBe('installed');
    expect(readlinkSync(join(linkDir, 'goosar'))).toBe(target);
  });

  it('never overwrites a foreign goosar (brew, manual install)', async () => {
    const foreign = join(root, 'homebrew', 'goosar');
    mkdirSync(join(root, 'homebrew'));
    writeFileSync(foreign, 'foreign');
    symlinkSync(foreign, join(linkDir, 'goosar'));
    const result = await installCliOnPath({ target, linkDir, ...unix });
    expect(result.state).toBe('foreign');
    if (result.state === 'foreign') expect(result.existing).toBe(foreign);
    expect(readlinkSync(join(linkDir, 'goosar'))).toBe(foreign);
  });

  it('never overwrites a foreign goosar whose link target is relative (Homebrew shape)', async () => {
    const cellar = join(root, 'Cellar', 'goosar', '1.0', 'bin');
    mkdirSync(cellar, { recursive: true });
    writeFileSync(join(cellar, 'goosar'), 'brew');
    symlinkSync('../Cellar/goosar/1.0/bin/goosar', join(linkDir, 'goosar'));
    const result = await installCliOnPath({ target, linkDir, ...unix });
    expect(result.state).toBe('foreign');
    if (result.state === 'foreign') expect(result.existing).toBe(join(cellar, 'goosar'));
    expect(readlinkSync(join(linkDir, 'goosar'))).toBe('../Cellar/goosar/1.0/bin/goosar');
  });

  it('treats a regular file at the link path as foreign too', async () => {
    writeFileSync(join(linkDir, 'goosar'), 'a real binary');
    const result = await installCliOnPath({ target, linkDir, ...unix });
    expect(result.state).toBe('foreign');
  });

  it.skipIf(process.getuid?.() === 0)(
    'reports no-permission with a copy-pasteable fix when the dir is read-only',
    async () => {
      chmodSync(linkDir, 0o555);
      const result = await installCliOnPath({ target, linkDir, ...unix });
      expect(result.state).toBe('no-permission');
      if (result.state === 'no-permission') {
        expect(result.hint).toContain('ln -sf');
        expect(result.hint).toContain(target);
      }
    },
  );

  it('reports no-permission when the link dir does not exist (no sudo, no mkdir)', async () => {
    rmSync(linkDir, { recursive: true });
    const result = await installCliOnPath({ target, linkDir, ...unix });
    expect(result.state).toBe('no-permission');
    expect(existsSync(linkDir)).toBe(false);
  });

  it('is unsupported on Windows and names the directory to put on PATH', async () => {
    const result = await installCliOnPath({ target, linkDir, platform: 'win32' });
    expect(result.state).toBe('unsupported');
    if (result.state === 'unsupported')
      expect(result.hint).toContain(join(root, 'Goosar.app', 'resources', 'bin'));
  });
});

describe('describeCliPathResult', () => {
  it('renders every state in English and Russian without leaking the state name', () => {
    const states = [
      { state: 'installed', link: '/l', target: '/t' },
      { state: 'already', link: '/l', target: '/t' },
      { state: 'foreign', link: '/l', existing: '/x' },
      { state: 'no-permission', link: '/l', target: '/t', hint: 'sudo ln -sf /t /l' },
      { state: 'unsupported', target: '/t', hint: 'C:\\dir' },
    ] as const;
    for (const s of states) {
      for (const lang of ['en', 'ru'] as const) {
        const text = describeCliPathResult(s, lang);
        expect(text.length).toBeGreaterThan(10);
        expect(text).not.toMatch(/no-permission|unsupported/);
      }
    }
    expect(describeCliPathResult(states[3], 'ru')).toContain('sudo ln -sf /t /l');
  });
});
