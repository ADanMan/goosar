import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { delimiter, dirname, join, resolve } from 'node:path';
import { afterEach, describe, it, expect } from 'vitest';
import {
  DESCRIBE_ARGS,
  builderArgsForTarget,
  bundlingEnv,
  deriveVersion,
  envWithLocalBins,
  findMacBuildArtifacts,
  normalizeGitVersion,
  parsePackageArgs,
  resolveBuildMatrix,
  stripLeadingSeparator,
} from './package.mjs';
import { SOURCE_ENV as MCP_SERVERS_SOURCE_ENV } from './bundle-mcp-servers.mjs';
import { PLAYWRIGHT_SOURCE_ENV, SKILLS_SOURCE_ENV } from './bundle-skills.mjs';

describe('normalizeGitVersion', () => {
  it('returns null for empty / nullish input', () => {
    expect(normalizeGitVersion('')).toBe(null);
    expect(normalizeGitVersion(null)).toBe(null);
    expect(normalizeGitVersion(undefined)).toBe(null);
  });

  it('strips the leading v on a clean tag', () => {
    expect(normalizeGitVersion('v0.1.36')).toBe('0.1.36');
    expect(normalizeGitVersion('v1.0.0')).toBe('1.0.0');
  });

  it('preserves the prerelease suffix between tags', () => {
    expect(normalizeGitVersion('v0.1.35-14-gf1415e96')).toBe('0.1.35-14-gf1415e96');
  });

  it('preserves the dirty suffix on a modified worktree', () => {
    expect(normalizeGitVersion('v0.1.35-14-gf1415e96-dirty')).toBe('0.1.35-14-gf1415e96-dirty');
  });

  it('handles v-prefixed prerelease tags', () => {
    expect(normalizeGitVersion('v1.0.0-alpha')).toBe('1.0.0-alpha');
    expect(normalizeGitVersion('v1.0.0-rc.2')).toBe('1.0.0-rc.2');
  });

  it('falls back to 0.0.0-g<hash> when no tags are reachable', () => {
    expect(normalizeGitVersion('f1415e96')).toBe('0.0.0-gf1415e96');
    expect(normalizeGitVersion('abc1234')).toBe('0.0.0-gabc1234');
    expect(normalizeGitVersion('2f24057b')).toBe('0.0.0-g2f24057b');
  });

  it('degrades a non-semver tag prefix that slips past the --match filter', () => {
    expect(normalizeGitVersion('release_iteration/Sprint_0705-3-g9adfcd4d8')).toBe(
      '0.0.0-grelease_iteration/Sprint_0705-3-g9adfcd4d8',
    );
    expect(normalizeGitVersion('v0.3.35-38-g9adfcd4d8')).toBe('0.3.35-38-g9adfcd4d8');
  });

  it('prefixes an all-digit hash so the pre-release is valid semver', () => {
    expect(normalizeGitVersion('0123456')).toBe('0.0.0-g0123456');
    expect(normalizeGitVersion('04567')).toBe('0.0.0-g04567');
  });
});

describe('DESCRIBE_ARGS', () => {
  it('passes the match pattern as one bare argv token, never a shell-quoted string', () => {
    expect(DESCRIBE_ARGS).toContain('v[0-9]*');
    for (const arg of DESCRIBE_ARGS) {
      expect(arg).not.toContain("'");
      expect(arg).not.toContain('"');
    }
  });
});

describe('deriveVersion (real git describe)', () => {
  const repos = [];

  function initRepo() {
    const dir = mkdtempSync(join(tmpdir(), 'goosar-desktop-ver-'));
    repos.push(dir);
    const run = (...args) => execFileSync('git', args, { cwd: dir, encoding: 'utf-8' });
    run('init', '-q');
    run('config', 'user.email', 'test@goosar.ru');
    run('config', 'user.name', 'test');
    run('config', 'commit.gpgsign', 'false');
    run('commit', '-q', '--allow-empty', '-m', 'root');
    return { dir, run };
  }

  afterEach(() => {
    while (repos.length) rmSync(repos.pop(), { recursive: true, force: true });
  });

  it('resolves a clean semver tag to its bare version', () => {
    const { dir, run } = initRepo();
    run('tag', 'v1.4.2');
    expect(deriveVersion(dir)).toBe('1.4.2');
  });

  it('selects the semver tag even when a nearer non-semver tag exists', () => {
    const { dir, run } = initRepo();
    run('tag', 'v1.4.2');
    run('commit', '-q', '--allow-empty', '-m', 'sprint');
    run('tag', 'release_iteration/Sprint_0705');
    const version = deriveVersion(dir);
    expect(version).toMatch(/^1\.4\.2-1-g[0-9a-f]+$/);
    expect(version).not.toMatch(/^0\.0\.0/);
  });

  it('falls back to 0.0.0-g<hash> when no semver tag is reachable', () => {
    const { dir } = initRepo();
    expect(deriveVersion(dir)).toMatch(/^0\.0\.0-g[0-9a-f]+$/);
  });
});

describe('stripLeadingSeparator', () => {
  it('removes the leading -- inserted by npm/pnpm', () => {
    expect(stripLeadingSeparator(['--', '--mac', '--arm64', '--publish', 'always'])).toEqual([
      '--mac',
      '--arm64',
      '--publish',
      'always',
    ]);
  });

  it('leaves args untouched when there is no leading --', () => {
    expect(stripLeadingSeparator(['--mac', '--arm64'])).toEqual(['--mac', '--arm64']);
  });

  it('does not strip a -- that appears mid-argv', () => {
    expect(stripLeadingSeparator(['--mac', '--', '--arm64'])).toEqual(['--mac', '--', '--arm64']);
  });

  it('handles an empty array', () => {
    expect(stripLeadingSeparator([])).toEqual([]);
  });
});

describe('parsePackageArgs', () => {
  it('collects per-platform targets and shared args', () => {
    expect(
      parsePackageArgs(['--win', 'nsis', '--mac', 'dmg', 'zip', '--arm64', '--publish', 'never']),
    ).toEqual({
      allPlatforms: false,
      sharedArgs: ['--publish', 'never'],
      platformTargets: {
        mac: ['dmg', 'zip'],
        win: ['nsis'],
        linux: [],
      },
      requestedPlatforms: ['win', 'mac'],
      requestedArchs: ['arm64'],
      requireMcpServers: false,
      requireSkills: false,
      thin: false,
    });
  });

  it('captures --require-mcp-servers / --require-skills without leaking them to electron-builder', () => {
    const parsed = parsePackageArgs([
      '--mac',
      '--arm64',
      '--require-mcp-servers',
      '--require-skills',
      '--publish',
      'always',
    ]);
    expect(parsed.requireMcpServers).toBe(true);
    expect(parsed.requireSkills).toBe(true);
    expect(parsed.sharedArgs).toEqual(['--publish', 'always']);
    expect(parsed.sharedArgs).not.toContain('--require-mcp-servers');
    expect(parsed.sharedArgs).not.toContain('--require-skills');
  });

  it('defaults the require flags to false', () => {
    const parsed = parsePackageArgs(['--mac', '--arm64']);
    expect(parsed.requireMcpServers).toBe(false);
    expect(parsed.requireSkills).toBe(false);
  });

  it('captures --thin without leaking it to electron-builder', () => {
    const parsed = parsePackageArgs(['--mac', '--arm64', '--thin', '--publish', 'always']);
    expect(parsed.thin).toBe(true);
    expect(parsed.sharedArgs).toEqual(['--publish', 'always']);
    expect(parsed.sharedArgs).not.toContain('--thin');
  });

  it('defaults thin to false', () => {
    expect(parsePackageArgs(['--mac', '--arm64']).thin).toBe(false);
  });

  it('rejects --thin combined with --require-mcp-servers', () => {
    expect(() => parsePackageArgs(['--mac', '--thin', '--require-mcp-servers'])).toThrow(
      /--thin cannot be combined with --require-mcp-servers/,
    );
  });

  it('rejects --thin combined with --require-skills', () => {
    expect(() => parsePackageArgs(['--mac', '--thin', '--require-skills'])).toThrow(
      /--thin cannot be combined with --require-mcp-servers/,
    );
  });

  it('expands combined short flags', () => {
    expect(parsePackageArgs(['-mw', '--x64']).requestedPlatforms).toEqual(['mac', 'win']);
  });

  it('tracks the all-platforms shortcut', () => {
    expect(parsePackageArgs(['--all-platforms', '--publish', 'never']).allPlatforms).toBe(true);
  });
});

describe('resolveBuildMatrix', () => {
  it('defaults to the current host platform and arch', () => {
    expect(
      resolveBuildMatrix(
        {
          allPlatforms: false,
          sharedArgs: [],
          platformTargets: { mac: [], win: [], linux: [] },
          requestedPlatforms: [],
          requestedArchs: [],
        },
        'darwin',
        'arm64',
      ),
    ).toEqual([{ platform: 'mac', arch: 'arm64' }]);
  });

  it('expands all-platforms on macOS', () => {
    expect(
      resolveBuildMatrix(
        {
          allPlatforms: true,
          sharedArgs: [],
          platformTargets: { mac: [], win: [], linux: [] },
          requestedPlatforms: [],
          requestedArchs: [],
        },
        'darwin',
        'arm64',
      ),
    ).toEqual([
      { platform: 'mac', arch: 'arm64' },
      { platform: 'mac', arch: 'x64' },
      { platform: 'win', arch: 'x64' },
      { platform: 'win', arch: 'arm64' },
      { platform: 'linux', arch: 'x64' },
      { platform: 'linux', arch: 'arm64' },
    ]);
  });

  it('rejects unsupported architectures', () => {
    expect(() =>
      resolveBuildMatrix(
        {
          allPlatforms: false,
          sharedArgs: [],
          platformTargets: { mac: [], win: [], linux: [] },
          requestedPlatforms: ['win'],
          requestedArchs: ['universal'],
        },
        'darwin',
        'arm64',
      ),
    ).toThrow(/unsupported Desktop CLI architecture/);
  });
});

describe('builderArgsForTarget', () => {
  it('adds scoped output directories for multi-target builds', () => {
    expect(
      builderArgsForTarget(
        { platform: 'win', arch: 'arm64' },
        {
          allPlatforms: false,
          sharedArgs: ['--publish', 'never'],
          platformTargets: { mac: [], win: ['nsis'], linux: [] },
          requestedPlatforms: ['win'],
          requestedArchs: ['arm64'],
        },
        '1.2.3',
        {
          disableMacNotarize: true,
          hostPlatform: 'darwin',
          useScopedOutputDir: true,
        },
      ),
    ).toEqual([
      '-c.extraMetadata.version=1.2.3',
      '-c.mac.notarize=false',
      '--win',
      'nsis',
      '--arm64',
      '--publish',
      'never',
      '-c.directories.output=dist/win-arm64',
      '-c.publish.channel=latest-arm64',
    ]);
  });

  it('does not override the publish channel for Windows x64 (default latest.yml)', () => {
    expect(
      builderArgsForTarget(
        { platform: 'win', arch: 'x64' },
        {
          allPlatforms: false,
          sharedArgs: ['--publish', 'always'],
          platformTargets: { mac: [], win: ['nsis'], linux: [] },
          requestedPlatforms: ['win'],
          requestedArchs: ['x64'],
        },
        '1.2.3',
        { hostPlatform: 'win32', useScopedOutputDir: true },
      ),
    ).toEqual([
      '-c.extraMetadata.version=1.2.3',
      '--win',
      'nsis',
      '--x64',
      '--publish',
      'always',
      '-c.directories.output=dist/win-x64',
    ]);
  });

  it('isolates the macOS x64 feed and platform floor', () => {
    expect(
      builderArgsForTarget(
        { platform: 'mac', arch: 'x64' },
        {
          allPlatforms: false,
          sharedArgs: ['--publish', 'always'],
          platformTargets: { mac: ['dmg', 'zip'], win: [], linux: [] },
          requestedPlatforms: ['mac'],
          requestedArchs: ['x64'],
        },
        '1.2.3',
        { hostPlatform: 'darwin', useScopedOutputDir: true },
      ),
    ).toEqual([
      '-c.extraMetadata.version=1.2.3',
      '--mac',
      'dmg',
      'zip',
      '--x64',
      '--publish',
      'always',
      '-c.directories.output=dist/mac-x64',
      '-c.mac.minimumSystemVersion=12.0.0',
      '-c.publish.channel=latest-x64',
    ]);
  });

  it('keeps macOS arm64 on the existing latest-mac update channel', () => {
    expect(
      builderArgsForTarget(
        { platform: 'mac', arch: 'arm64' },
        {
          allPlatforms: false,
          sharedArgs: ['--publish', 'always'],
          platformTargets: { mac: [], win: [], linux: [] },
          requestedPlatforms: ['mac'],
          requestedArchs: ['arm64'],
        },
        '1.2.3',
        { hostPlatform: 'darwin', useScopedOutputDir: true },
      ),
    ).toEqual([
      '-c.extraMetadata.version=1.2.3',
      '--mac',
      '--arm64',
      '--publish',
      'always',
      '-c.directories.output=dist/mac-arm64',
    ]);
  });

  it('defaults linux cross-builds to AppImage on non-Linux hosts', () => {
    expect(
      builderArgsForTarget(
        { platform: 'linux', arch: 'x64' },
        {
          allPlatforms: false,
          sharedArgs: ['--publish', 'never'],
          platformTargets: { mac: [], win: [], linux: [] },
          requestedPlatforms: ['linux'],
          requestedArchs: ['x64'],
        },
        '1.2.3',
        { hostPlatform: 'darwin' },
      ),
    ).toEqual([
      '-c.extraMetadata.version=1.2.3',
      '--linux',
      'AppImage',
      '--x64',
      '--publish',
      'never',
    ]);
  });
});

describe('envWithLocalBins', () => {
  it('prepends desktop-local binary directories to PATH', () => {
    const desktopRoot = '/repo/apps/desktop';
    const result = envWithLocalBins(
      { PATH: ['/usr/local/bin', '/usr/bin'].join(delimiter) },
      desktopRoot,
    );
    expect(result.PATH.split(delimiter)).toEqual([
      resolve(desktopRoot, 'node_modules', '.bin'),
      resolve(desktopRoot, '..', '..', 'node_modules', '.bin'),
      '/usr/local/bin',
      '/usr/bin',
    ]);
  });

  it('preserves an existing Path key and avoids duplicate entries', () => {
    const desktopRoot = '/repo/apps/desktop';
    const desktopBin = resolve(desktopRoot, 'node_modules', '.bin');
    const workspaceBin = resolve(desktopRoot, '..', '..', 'node_modules', '.bin');
    const result = envWithLocalBins(
      { Path: [desktopBin, 'runner-bin', workspaceBin].join(delimiter) },
      desktopRoot,
    );
    expect(result).not.toHaveProperty('PATH');
    expect(result.Path.split(delimiter)).toEqual([desktopBin, workspaceBin, 'runner-bin']);
  });
});

describe('bundlingEnv (issue #185 thin build profile)', () => {
  const desktopRoot = '/repo/apps/desktop';

  it('passes the env through unchanged when not thin', () => {
    const env = {
      PATH: '/usr/bin',
      [MCP_SERVERS_SOURCE_ENV]: '/corp/mcp-servers',
      [SKILLS_SOURCE_ENV]: '/corp/skills',
      [PLAYWRIGHT_SOURCE_ENV]: '/corp/playwright',
    };
    const result = bundlingEnv(false, env, desktopRoot);
    expect(result[MCP_SERVERS_SOURCE_ENV]).toBe('/corp/mcp-servers');
    expect(result[SKILLS_SOURCE_ENV]).toBe('/corp/skills');
    expect(result[PLAYWRIGHT_SOURCE_ENV]).toBe('/corp/playwright');
  });

  it('strips the bundler source-dir env vars when thin, regardless of what the build machine has configured', () => {
    const env = {
      PATH: '/usr/bin',
      [MCP_SERVERS_SOURCE_ENV]: '/corp/mcp-servers',
      [SKILLS_SOURCE_ENV]: '/corp/skills',
      [PLAYWRIGHT_SOURCE_ENV]: '/corp/playwright',
    };
    const result = bundlingEnv(true, env, desktopRoot);
    expect(result).not.toHaveProperty(MCP_SERVERS_SOURCE_ENV);
    expect(result).not.toHaveProperty(SKILLS_SOURCE_ENV);
    expect(result).not.toHaveProperty(PLAYWRIGHT_SOURCE_ENV);
  });

  it('strips the preset-overlay source env when thin (issue #161)', () => {
    const env = {
      PATH: '/usr/bin',
      GOOSAR_PRESET_OVERLAY: '/corp/overlay.json',
    };
    const result = bundlingEnv(true, env, desktopRoot);
    expect(result).not.toHaveProperty('GOOSAR_PRESET_OVERLAY');
    expect(bundlingEnv(false, env, desktopRoot).GOOSAR_PRESET_OVERLAY).toBe('/corp/overlay.json');
  });

  it('strips the deployment-defaults source env when thin (issue #438)', () => {
    const env = {
      PATH: '/usr/bin',
      GOOSAR_DESKTOP_DEPLOYMENT_DEFAULTS: '/corp/deployment.json',
    };
    expect(bundlingEnv(true, env, desktopRoot)).not.toHaveProperty(
      'GOOSAR_DESKTOP_DEPLOYMENT_DEFAULTS',
    );
    expect(bundlingEnv(false, env, desktopRoot).GOOSAR_DESKTOP_DEPLOYMENT_DEFAULTS).toBe(
      '/corp/deployment.json',
    );
  });

  it('thin mode still carries the local-bins PATH prepend (does not bypass envWithLocalBins)', () => {
    const env = { PATH: '/usr/bin' };
    const result = bundlingEnv(true, env, desktopRoot);
    expect(result.PATH.split(delimiter)[0]).toBe(resolve(desktopRoot, 'node_modules', '.bin'));
  });

  it('thin mode is a no-op when the source env vars were never set', () => {
    const env = { PATH: '/usr/bin' };
    const result = bundlingEnv(true, env, desktopRoot);
    expect(result).not.toHaveProperty(MCP_SERVERS_SOURCE_ENV);
    expect(result).not.toHaveProperty(SKILLS_SOURCE_ENV);
    expect(result).not.toHaveProperty(PLAYWRIGHT_SOURCE_ENV);
  });
});

describe('electron-builder.yml packaging config', () => {
  const configPath = [
    resolve(process.cwd(), 'electron-builder.yml'),
    resolve(process.cwd(), 'apps/desktop/electron-builder.yml'),
  ].find((candidate) => existsSync(candidate));

  function readFilesBlock(raw) {
    const lines = raw.split('\n');
    const start = lines.findIndex((l) => /^files:\s*$/.test(l));
    if (start === -1) return [];
    const entries = [];
    for (let i = start + 1; i < lines.length; i += 1) {
      const line = lines[i];
      if (/^\S/.test(line)) break; 
      const trimmed = line.trim();
      if (trimmed === '' || trimmed.startsWith('#')) continue;
      const m = trimmed.match(/^-\s*"?(.*?)"?\s*$/);
      if (m) entries.push(m[1]);
    }
    return entries;
  }

  it('excludes the dist output directory from the packaged files', () => {
    expect(configPath, 'electron-builder.yml not found').toBeTruthy();
    const entries = readFilesBlock(readFileSync(configPath, 'utf-8'));
    expect(entries.length).toBeGreaterThan(0);
    expect(entries).toContain('!dist/**');
  });
});

describe('bundle verifier wiring (issue #162)', () => {
  const scriptsDir = [
    resolve(process.cwd(), 'scripts'),
    resolve(process.cwd(), 'apps/desktop/scripts'),
  ].find((c) => existsSync(join(c, 'verify-mcp-server.mjs')));
  const verifyScript = scriptsDir ? join(scriptsDir, 'verify-mcp-server.mjs') : null;

  let work;
  afterEach(() => {
    if (work) rmSync(work, { recursive: true, force: true });
    work = undefined;
  });

  function runVerifyAll(stagingDir) {
    try {
      execFileSync('node', [verifyScript, '--all', stagingDir], {
        stdio: 'pipe',
      });
      return 0;
    } catch (err) {
      return err.status ?? 1;
    }
  }

  it('exits 0 when nothing is staged (no-op for public builds)', () => {
    expect(verifyScript, 'verify-mcp-server.mjs not found').toBeTruthy();
    work = mkdtempSync(join(tmpdir(), 'pkg-verify-empty-'));
    writeFileSync(join(work, 'MCP-SERVERS-NOT-BUNDLED.txt'), 'none\n');
    expect(runVerifyAll(work)).toBe(0);
  });

  it('exits non-zero when a staged server fails to come up (fail-closed)', () => {
    expect(verifyScript, 'verify-mcp-server.mjs not found').toBeTruthy();
    work = mkdtempSync(join(tmpdir(), 'pkg-verify-bad-'));
    const serverDir = join(work, 'broken');
    mkdirSync(serverDir, { recursive: true });
    const crash = join(serverDir, 'crash.mjs');
    writeFileSync(crash, 'process.stderr.write("boom\\n"); process.exit(1);');
    writeFileSync(
      join(serverDir, 'mcp-server.json'),
      JSON.stringify({ command: process.execPath, args: [crash] }),
    );
    expect(runVerifyAll(work)).not.toBe(0);
  });
});

describe('findMacBuildArtifacts', () => {
  function layout(root, spec) {
    for (const [rel, kind] of Object.entries(spec)) {
      const abs = join(root, rel);
      if (kind === 'dir') mkdirSync(abs, { recursive: true });
      else {
        mkdirSync(dirname(abs), { recursive: true });
        writeFileSync(abs, 'x');
      }
    }
    return root;
  }

  it('finds the nested .app and scoped zip of a multi-arch (scoped) build', () => {
    const dist = layout(mkdtempSync(join(tmpdir(), 'pkg-disc-')), {
      'mac-x64/mac/Goosar.app/Contents/Info.plist': 'file',
      'mac-x64/goosar-desktop-0.7.0-mac-x64.zip': 'file',
    });
    const found = findMacBuildArtifacts({
      distRoot: dist,
      platform: 'mac',
      arch: 'x64',
      scoped: true,
    });
    expect(found.appPath).toBe(join(dist, 'mac-x64/mac/Goosar.app'));
    expect(found.zipPath).toBe(join(dist, 'mac-x64/goosar-desktop-0.7.0-mac-x64.zip'));
    expect(found.dmgPath).toBe(join(dist, 'goosar-desktop-0.7.0-mac-x64.dmg'));
  });

  it('finds an arch-suffixed single-arch build (dist/mac-arm64/App.app)', () => {
    const dist = layout(mkdtempSync(join(tmpdir(), 'pkg-disc-')), {
      'mac-arm64/Goosar.app/Contents/Info.plist': 'file',
      'goosar-desktop-0.7.0-mac-arm64.zip': 'file',
    });
    const found = findMacBuildArtifacts({
      distRoot: dist,
      platform: 'mac',
      arch: 'arm64',
      scoped: false,
    });
    expect(found.appPath).toBe(join(dist, 'mac-arm64/Goosar.app'));
    expect(found.dmgPath).toBe(join(dist, 'goosar-desktop-0.7.0-mac-arm64.dmg'));
  });

  it('finds a single-arch x64 build written to dist/mac with NO arch suffix', () => {
    const dist = layout(mkdtempSync(join(tmpdir(), 'pkg-disc-')), {
      'mac/Goosar.app/Contents/Info.plist': 'file',
      'goosar-desktop-0.7.0-mac-x64.zip': 'file',
    });
    const found = findMacBuildArtifacts({
      distRoot: dist,
      platform: 'mac',
      arch: 'x64',
      scoped: false,
    });
    expect(found.appPath).toBe(join(dist, 'mac/Goosar.app'));
  });

  it('prefers the arch-tagged zip when several are present', () => {
    const dist = layout(mkdtempSync(join(tmpdir(), 'pkg-disc-')), {
      'mac-arm64/Goosar.app/Contents/Info.plist': 'file',
      'goosar-desktop-0.7.0-mac-x64.zip': 'file',
      'goosar-desktop-0.7.0-mac-arm64.zip': 'file',
    });
    const found = findMacBuildArtifacts({
      distRoot: dist,
      platform: 'mac',
      arch: 'arm64',
      scoped: false,
    });
    expect(found.zipPath).toBe(join(dist, 'goosar-desktop-0.7.0-mac-arm64.zip'));
  });

  it('reports no app (instead of throwing) when the directory is missing', () => {
    const dist = mkdtempSync(join(tmpdir(), 'pkg-disc-'));
    const found = findMacBuildArtifacts({
      distRoot: dist,
      platform: 'mac',
      arch: 'x64',
      scoped: true,
    });
    expect(found.appPath).toBeNull();
  });

  it('never descends into a .app bundle looking for artifacts', () => {
    const dist = layout(mkdtempSync(join(tmpdir(), 'pkg-disc-')), {
      'mac-arm64/Goosar.app/Contents/Resources/decoy-mac-arm64.zip': 'file',
      'mac-arm64/goosar-desktop-0.7.0-mac-arm64.zip': 'file',
    });
    const found = findMacBuildArtifacts({
      distRoot: dist,
      platform: 'mac',
      arch: 'arm64',
      scoped: false,
    });
    expect(found.zipPath).toBe(join(dist, 'mac-arm64/goosar-desktop-0.7.0-mac-arm64.zip'));
  });
});
