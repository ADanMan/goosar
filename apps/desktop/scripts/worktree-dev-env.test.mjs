import { mkdtempSync, rmSync, writeFileSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, describe, expect, it } from 'vitest';

import {
  appSuffixForPath,
  applyWorktreeDevEnv,
  cksum,
  offsetForPath,
  rendererPortForPath,
} from './worktree-dev-env.mjs';

const cleanups = [];
afterEach(() => {
  while (cleanups.length) cleanups.pop()();
});

function tmpRoot(kind ) {
  const root = mkdtempSync(join(tmpdir(), 'wt-'));
  cleanups.push(() => rmSync(root, { recursive: true, force: true }));
  if (kind === 'file') writeFileSync(join(root, '.git'), 'gitdir: /elsewhere\n');
  else if (kind === 'dir') mkdirSync(join(root, '.git'));
  return root;
}

describe('worktree-dev-env', () => {
  it('cksum is byte-compatible with coreutils cksum(1)', () => {
    expect(cksum(Buffer.from('/tmp/foo'))).toBe(427878967);
    expect(cksum(Buffer.from(''))).toBe(4294967295);
  });

  it('derives the offset from the path, mod 1000', () => {
    expect(offsetForPath('/tmp/foo')).toBe(427878967 % 1000);
  });

  it('renderer port is 5174 + offset (5172 reserved for the primary checkout)', () => {
    expect(rendererPortForPath('/tmp/foo')).toBe(5174 + (427878967 % 1000));
  });

  it('never reuses the primary 5172 even when the offset is 0', () => {
    expect(offsetForPath('/tmp/goosar-259')).toBe(973);
    expect(rendererPortForPath('/tmp/goosar-259')).toBe(6147);
    expect(rendererPortForPath('/tmp/goosar-259')).not.toBe(5172);
  });

  it("suffix is '<folder>-<offset>' so it stays recognizable and unique", () => {
    expect(appSuffixForPath('/work/MUL-3724_Desktop')).toBe(
      `mul-3724-desktop-${offsetForPath('/work/MUL-3724_Desktop')}`,
    );
    expect(appSuffixForPath('/work/feat/some thing')).toBe(
      `some-thing-${offsetForPath('/work/feat/some thing')}`,
    );
    expect(appSuffixForPath('/work/___')).toBe(`worktree-${offsetForPath('/work/___')}`);
  });

  it('disambiguates worktrees that share a folder name at different paths', () => {
    expect(offsetForPath('/tmp/a/goosar')).not.toBe(offsetForPath('/tmp/b/goosar'));
    expect(appSuffixForPath('/tmp/a/goosar')).not.toBe(appSuffixForPath('/tmp/b/goosar'));
  });

  it('auto-isolates a linked worktree (.git is a file)', () => {
    const root = tmpRoot('file');
    const env = {};
    applyWorktreeDevEnv(env, { root });
    expect(env.DESKTOP_RENDERER_PORT).toBe(String(rendererPortForPath(root)));
    expect(env.DESKTOP_APP_SUFFIX).toBe(appSuffixForPath(root));
  });

  it('leaves the primary checkout untouched (.git is a dir)', () => {
    const root = tmpRoot('dir');
    const env = {};
    applyWorktreeDevEnv(env, { root });
    expect(env.DESKTOP_RENDERER_PORT).toBeUndefined();
    expect(env.DESKTOP_APP_SUFFIX).toBeUndefined();
  });

  it('respects explicit env overrides', () => {
    const root = tmpRoot('file');
    const env = { DESKTOP_RENDERER_PORT: '9999', DESKTOP_APP_SUFFIX: 'manual' };
    applyWorktreeDevEnv(env, { root });
    expect(env.DESKTOP_RENDERER_PORT).toBe('9999');
    expect(env.DESKTOP_APP_SUFFIX).toBe('manual');
  });

  it('fills only the missing knob when one is set explicitly', () => {
    const root = tmpRoot('file');
    const env = { DESKTOP_RENDERER_PORT: '9999' };
    applyWorktreeDevEnv(env, { root });
    expect(env.DESKTOP_RENDERER_PORT).toBe('9999');
    expect(env.DESKTOP_APP_SUFFIX).toBe(appSuffixForPath(root));
  });
});
