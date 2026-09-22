// Тесты проверки упакованного артефакта.
import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterAll, describe, expect, it } from 'vitest';

import {
  compareInventories,
  findDanglingSymlinks,
  listZipInventory,
  resolvePathInInventory,
  verifyDmgArtifact,
  verifyZipArtifact,
  walkTree,
} from './verify-packaged-artifact.mjs';

const isDarwin = process.platform === 'darwin';
const tempRoots = [];

function makeTempDir() {
  const dir = mkdtempSync(join(tmpdir(), 'goosar-verify-test-'));
  tempRoots.push(dir);
  return dir;
}

afterAll(() => {
  for (const dir of tempRoots) rmSync(dir, { recursive: true, force: true });
});

function buildFixtureApp(root, { includeFrameworkBinary = true } = {}) {
  const app = join(root, 'Fixture.app');
  const versionsA = join(app, 'Contents/Frameworks/Foo.framework/Versions/A');
  mkdirSync(versionsA, { recursive: true });
  mkdirSync(join(app, 'Contents/MacOS'), { recursive: true });
  writeFileSync(join(app, 'Contents/MacOS/Fixture'), 'stub-executable');
  writeFileSync(join(app, 'Contents/Info.plist'), '<plist/>');
  if (includeFrameworkBinary) {
    writeFileSync(join(versionsA, 'Foo'), 'X'.repeat(4096));
  }
  symlinkSync('A', join(app, 'Contents/Frameworks/Foo.framework/Versions/Current'));
  symlinkSync('Versions/Current/Foo', join(app, 'Contents/Frameworks/Foo.framework/Foo'));
  return app;
}

function buildDmg(appPath, dmgPath, volname = 'Fixture') {
  execFileSync(
    'hdiutil',
    ['create', '-size', '16m', '-fs', 'HFS+', '-volname', volname, '-type', 'UDIF', dmgPath],
    { stdio: 'ignore' },
  );
  const out = execFileSync(
    'hdiutil',
    ['attach', '-nobrowse', '-noverify', '-mountrandom', '/tmp', dmgPath],
    { encoding: 'utf-8' },
  );
  const mount = out
    .split('\n')
    .map((l) => l.split('\t').pop()?.trim() ?? '')
    .filter((p) => p.startsWith('/'))
    .pop();
  execFileSync('ditto', [appPath, join(mount, 'Fixture.app')], { stdio: 'ignore' });
  execFileSync('hdiutil', ['detach', mount, '-quiet'], { stdio: 'ignore' });
  return dmgPath;
}

describe('walkTree', () => {
  it('records symlinks with their targets instead of following them', () => {
    const app = buildFixtureApp(makeTempDir());
    const inv = walkTree(app);
    expect(inv.get('Contents/Frameworks/Foo.framework/Foo')).toEqual({
      kind: 'link',
      size: 0,
      target: 'Versions/Current/Foo',
    });
    expect(inv.get('Contents/Frameworks/Foo.framework/Versions/A/Foo')).toEqual({
      kind: 'file',
      size: 4096,
      target: null,
    });
  });
});

describe('resolvePathInInventory', () => {
  it('follows a multi-hop framework chain to the real binary', () => {
    const inv = walkTree(buildFixtureApp(makeTempDir()));
    const resolved = resolvePathInInventory(inv, 'Contents/Frameworks/Foo.framework/Foo');
    expect(resolved).toEqual({ kind: 'file', size: 4096, target: null });
  });

  it('reports a chain that dead-ends', () => {
    const inv = walkTree(buildFixtureApp(makeTempDir(), { includeFrameworkBinary: false }));
    expect(resolvePathInInventory(inv, 'Contents/Frameworks/Foo.framework/Foo')).toBeNull();
  });

  it('treats an absolute target as external rather than dangling', () => {
    const root = makeTempDir();
    const app = buildFixtureApp(root);
    symlinkSync('/Applications', join(app, 'Contents/Shortcut'));
    const inv = walkTree(app);
    expect(resolvePathInInventory(inv, 'Contents/Shortcut')?.kind).toBe('external');
  });
});

describe('findDanglingSymlinks', () => {
  it('finds none in a healthy framework layout', () => {
    expect(findDanglingSymlinks(walkTree(buildFixtureApp(makeTempDir())))).toEqual([]);
  });

  it('flags the framework entry point when the binary is gone', () => {
    const inv = walkTree(buildFixtureApp(makeTempDir(), { includeFrameworkBinary: false }));
    expect(findDanglingSymlinks(inv)).toContain('Contents/Frameworks/Foo.framework/Foo');
  });
});

describe('compareInventories', () => {
  it('accepts an identical copy', () => {
    const app = buildFixtureApp(makeTempDir());
    expect(compareInventories(walkTree(app), walkTree(app))).toEqual([]);
  });

  it('rejects a file that is missing from the artifact', () => {
    const source = walkTree(buildFixtureApp(makeTempDir()));
    const artifact = walkTree(buildFixtureApp(makeTempDir(), { includeFrameworkBinary: false }));
    const problems = compareInventories(source, artifact);
    expect(
      problems.some((p) => p.startsWith('MISSING in artifact:') && p.includes('Versions/A/Foo')),
    ).toBe(true);
  });

  it('rejects a short (truncated) file', () => {
    const source = walkTree(buildFixtureApp(makeTempDir()));
    const shortRoot = makeTempDir();
    const shortApp = buildFixtureApp(shortRoot, { includeFrameworkBinary: false });
    writeFileSync(
      join(shortApp, 'Contents/Frameworks/Foo.framework/Versions/A/Foo'),
      'X'.repeat(10),
    );
    const problems = compareInventories(source, walkTree(shortApp));
    expect(problems.some((p) => p.startsWith('SIZE MISMATCH:'))).toBe(true);
  });

  it('rejects a retargeted symlink', () => {
    const source = walkTree(buildFixtureApp(makeTempDir()));
    const root = makeTempDir();
    const app = join(root, 'Fixture.app');
    mkdirSync(join(app, 'Contents/Frameworks/Foo.framework/Versions/A'), { recursive: true });
    mkdirSync(join(app, 'Contents/MacOS'), { recursive: true });
    writeFileSync(join(app, 'Contents/MacOS/Fixture'), 'stub-executable');
    writeFileSync(join(app, 'Contents/Info.plist'), '<plist/>');
    writeFileSync(join(app, 'Contents/Frameworks/Foo.framework/Versions/A/Foo'), 'X'.repeat(4096));
    symlinkSync('A', join(app, 'Contents/Frameworks/Foo.framework/Versions/Current'));
    symlinkSync('Versions/A/Foo', join(app, 'Contents/Frameworks/Foo.framework/Foo'));
    expect(
      compareInventories(source, walkTree(app)).some((p) =>
        p.startsWith('SYMLINK TARGET CHANGED:'),
      ),
    ).toBe(true);
  });
});

const ARTIFACT_TOOL_TIMEOUT_MS = 120_000;

describe.skipIf(!isDarwin)('verifyDmgArtifact', () => {
  it(
    'accepts a DMG that carries the whole app',
    () => {
      const root = makeTempDir();
      const app = buildFixtureApp(root);
      const dmg = buildDmg(app, join(root, 'good.dmg'));
      expect(verifyDmgArtifact(app, dmg)).toEqual({ ok: true, problems: [] });
    },
    ARTIFACT_TOOL_TIMEOUT_MS,
  );

  it(
    'rejects a DMG whose framework binary never made it into the image',
    () => {
      const sourceRoot = makeTempDir();
      const truncatedRoot = makeTempDir();
      const sourceApp = buildFixtureApp(sourceRoot);
      const truncatedApp = buildFixtureApp(truncatedRoot, { includeFrameworkBinary: false });
      const dmg = buildDmg(truncatedApp, join(truncatedRoot, 'truncated.dmg'));
      const result = verifyDmgArtifact(sourceApp, dmg);
      expect(result.ok).toBe(false);
      expect(result.problems.some((p) => p.includes('Versions/A/Foo'))).toBe(true);
      expect(result.problems.some((p) => p.startsWith('DANGLING SYMLINK in artifact:'))).toBe(true);
    },
    ARTIFACT_TOOL_TIMEOUT_MS,
  );
});

describe.skipIf(!isDarwin)('verifyZipArtifact', () => {
  function buildZip(appPath, zipPath) {
    execFileSync('ditto', ['-c', '-k', '--sequesterRsrc', '--keepParent', appPath, zipPath], {
      stdio: 'ignore',
    });
    return zipPath;
  }

  it(
    'accepts a zip that carries the whole app',
    () => {
      const root = makeTempDir();
      const app = buildFixtureApp(root);
      expect(verifyZipArtifact(app, buildZip(app, join(root, 'good.zip'))).ok).toBe(true);
    },
    ARTIFACT_TOOL_TIMEOUT_MS,
  );

  it(
    'rejects a zip missing the framework binary',
    () => {
      const sourceApp = buildFixtureApp(makeTempDir());
      const truncatedRoot = makeTempDir();
      const truncatedApp = buildFixtureApp(truncatedRoot, { includeFrameworkBinary: false });
      const result = verifyZipArtifact(
        sourceApp,
        buildZip(truncatedApp, join(truncatedRoot, 'bad.zip')),
      );
      expect(result.ok).toBe(false);
      expect(result.problems.some((p) => p.includes('Versions/A/Foo'))).toBe(true);
    },
    ARTIFACT_TOOL_TIMEOUT_MS,
  );

  it(
    'inventories only the app subtree',
    () => {
      const root = makeTempDir();
      const app = buildFixtureApp(root);
      const inv = listZipInventory(buildZip(app, join(root, 'x.zip')), 'Fixture.app');
      expect(inv.has('Contents/Info.plist')).toBe(true);
      expect([...inv.keys()].every((k) => !k.startsWith('Fixture.app/'))).toBe(true);
    },
    ARTIFACT_TOOL_TIMEOUT_MS,
  );
});
