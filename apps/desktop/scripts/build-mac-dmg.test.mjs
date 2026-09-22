// Тесты безопасного сборщика DMG.
import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterAll, describe, expect, it } from 'vitest';

import { IMAGE_HEADROOM_MB, buildMacDmg, computeImageSizeMb } from './build-mac-dmg.mjs';
import { verifyDmgArtifact } from './verify-packaged-artifact.mjs';

const isDarwin = process.platform === 'darwin';
const tempRoots = [];

function makeTempDir() {
  const dir = mkdtempSync(join(tmpdir(), 'goosar-dmgbuild-test-'));
  tempRoots.push(dir);
  return dir;
}

afterAll(() => {
  for (const dir of tempRoots) rmSync(dir, { recursive: true, force: true });
});

function buildFixtureApp(root, { payloadBytes = 64 * 1024 } = {}) {
  const app = join(root, 'Fixture.app');
  const versionsA = join(app, 'Contents/Frameworks/Foo.framework/Versions/A');
  mkdirSync(versionsA, { recursive: true });
  mkdirSync(join(app, 'Contents/MacOS'), { recursive: true });
  writeFileSync(join(app, 'Contents/MacOS/Fixture'), 'stub-executable');
  writeFileSync(join(versionsA, 'Foo'), 'X'.repeat(payloadBytes));
  symlinkSync('A', join(app, 'Contents/Frameworks/Foo.framework/Versions/Current'));
  symlinkSync('Versions/Current/Foo', join(app, 'Contents/Frameworks/Foo.framework/Foo'));
  return app;
}

describe.skipIf(!isDarwin)('computeImageSizeMb', () => {
  it('adds headroom on top of the measured app size', () => {
    const app = buildFixtureApp(makeTempDir());
    const measured = Number.parseInt(
      execFileSync('du', ['-sm', app], { encoding: 'utf-8' }).trim().split(/\s+/)[0],
      10,
    );
    expect(computeImageSizeMb(app)).toBe(measured + IMAGE_HEADROOM_MB);
    expect(computeImageSizeMb(app, 5)).toBe(measured + 5);
  });

  it('defaults to headroom generous enough for the fat profile overrun that caused #186', () => {
    expect(IMAGE_HEADROOM_MB).toBeGreaterThanOrEqual(500);
  });
});

describe.skipIf(!isDarwin)('buildMacDmg', () => {
  it('produces a DMG that passes independent verification', () => {
    const root = makeTempDir();
    const app = buildFixtureApp(root);
    const out = join(root, 'fixture.dmg');
    buildMacDmg({ appPath: app, outDmg: out, volname: 'Fixture', headroomMb: 12, log: () => {} });
    expect(existsSync(out)).toBe(true);
    expect(verifyDmgArtifact(app, out)).toEqual({ ok: true, problems: [] });
  }, 120_000);

  it('carries the /Applications drag-install symlink', () => {
    const root = makeTempDir();
    const app = buildFixtureApp(root);
    const out = join(root, 'fixture.dmg');
    buildMacDmg({ appPath: app, outDmg: out, volname: 'Fixture', headroomMb: 12, log: () => {} });
    const attach = execFileSync(
      'hdiutil',
      ['attach', '-readonly', '-nobrowse', '-noverify', '-mountrandom', '/tmp', out],
      { encoding: 'utf-8' },
    );
    const mount = attach
      .split('\n')
      .map((l) => l.split('\t').pop()?.trim() ?? '')
      .filter((p) => p.startsWith('/'))
      .pop();
    try {
      expect(existsSync(join(mount, 'Applications'))).toBe(true);
    } finally {
      execFileSync('hdiutil', ['detach', mount, '-quiet'], { stdio: 'ignore' });
    }
  }, 120_000);

  it('refuses to write an output DMG when the app does not fit in the image', () => {
    const root = makeTempDir();
    const app = buildFixtureApp(root, { payloadBytes: 24 * 1024 * 1024 });
    const out = join(root, 'toosmall.dmg');
    expect(() =>
      buildMacDmg({
        appPath: app,
        outDmg: out,
        volname: 'Fixture',
        headroomMb: -20,
        log: () => {},
      }),
    ).toThrow();
    expect(existsSync(out)).toBe(false);
  }, 120_000);
});
