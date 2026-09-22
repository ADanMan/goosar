import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { execFileSync, spawnSync } from 'node:child_process';
import {
  chmodSync,
  existsSync,
  lstatSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readlinkSync,
  realpathSync,
  rmSync,
  writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import {
  artifactNameMatches,
  hashArtifactTree,
  inspectArtifact,
  inspectMcpClient,
  mainCheckoutRoot,
} from './bundle-agent.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const script = join(here, 'bundle-agent.mjs');
const desktopRoot = resolve(here, '..');
const stagingRoot = join(desktopRoot, 'resources-agent');
const stagingDir = join(stagingRoot, 'hermes');

let work;

function makeArtifact(name, version, { withInstaller = true } = {}) {
  const dir = join(work, name);
  mkdirSync(join(dir, 'bin'), { recursive: true });
  mkdirSync(join(dir, 'venv', 'bin'), { recursive: true });
  const bin = join(dir, 'bin', 'hermes');
  writeFileSync(bin, '#!/bin/sh\necho hermes\n');
  chmodSync(bin, 0o755);
  if (withInstaller) {
    const installer = join(dir, 'bin', 'install-artifact.sh');
    writeFileSync(installer, '#!/usr/bin/env bash\nexit 0\n');
    chmodSync(installer, 0o755);
  }
  writeFileSync(join(dir, 'VERSION'), `${version}\n`);
  execFileSync('ln', ['-s', '../../python/bin/python3.13', join(dir, 'venv', 'bin', 'python')]);
  return dir;
}

function readManifest(dir) {
  const text = readFileSync(join(dir, 'ARTIFACT-MANIFEST.txt'), 'utf-8');
  const fields = {};
  for (const line of text.split('\n')) {
    const match = line.match(/^([a-z0-9_]+)=(.*)$/);
    if (match) fields[match[1]] = match[2];
  }
  return fields;
}

function runScript(env = {}, args = []) {
  const result = spawnSync('node', [script, ...args], {
    cwd: desktopRoot,
    encoding: 'utf-8',
    env: { ...process.env, ...env },
  });
  const output = `${result.stdout ?? ''}${result.stderr ?? ''}`;
  if (result.status !== 0) {
    throw new Error(`bundle-agent exited ${result.status}: ${output}`);
  }
  return output;
}

beforeEach(() => {
  work = mkdtempSync(join(tmpdir(), 'bundle-agent-'));
  rmSync(stagingRoot, { recursive: true, force: true });
});

afterEach(() => {
  rmSync(work, { recursive: true, force: true });
  rmSync(stagingRoot, { recursive: true, force: true });
});

describe('artifactNameMatches', () => {
  it('matches the uname tuple build-artifact.sh produces', () => {
    expect(artifactNameMatches('hermes-0.13.0-darwin-arm64', 'darwin', 'arm64')).toBe(true);
    expect(artifactNameMatches('hermes-0.13.0-linux-x86_64', 'linux', 'x64')).toBe(true);
    expect(artifactNameMatches('hermes-0.13.0-linux-aarch64', 'linux', 'arm64')).toBe(true);
  });

  it('rejects another platform or architecture', () => {
    expect(artifactNameMatches('hermes-0.13.0-darwin-arm64', 'darwin', 'x64')).toBe(false);
    expect(artifactNameMatches('hermes-0.13.0-linux-x86_64', 'darwin', 'x64')).toBe(false);
    expect(artifactNameMatches('something-else', 'darwin', 'arm64')).toBe(false);
  });
});

describe('inspectArtifact', () => {
  it('accepts a complete artifact', async () => {
    const dir = makeArtifact('hermes-1.2.3-darwin-arm64', '1.2.3');
    await expect(inspectArtifact(dir)).resolves.toMatchObject({
      ok: true,
      version: '1.2.3',
    });
  });

  it('rejects an artifact without its own installer', async () => {
    const dir = makeArtifact('hermes-1.2.3-darwin-arm64', '1.2.3', {
      withInstaller: false,
    });
    const result = await inspectArtifact(dir);
    expect(result.ok).toBe(false);
    expect(result.reason).toMatch(/install-artifact\.sh/);
  });

  it('rejects a missing directory', async () => {
    const result = await inspectArtifact(join(work, 'nope'));
    expect(result.ok).toBe(false);
  });
});

describe('inspectArtifact — the MCP client gate is wired in', () => {
  it('refuses a structurally complete artifact whose MCP client is incompatible', async () => {
    const dir = makeArtifact('hermes-9.9.9-darwin-arm64', '9.9.9');
    const python = join(dir, 'venv', 'bin', 'python');
    rmSync(python, { force: true });
    writeFileSync(
      python,
      `#!/bin/sh\ncat <<'JSON'\n${JSON.stringify({
        state: 'incompatible',
        version: '2.0.0',
        tool_fields: ['input_schema', 'name'],
      })}\nJSON\n`,
    );
    chmodSync(python, 0o755);

    const result = await inspectArtifact(dir);
    expect(result.ok).toBe(false);
    expect(result.reason).toContain('2.0.0');
  });
});

describe('inspectMcpClient', () => {
  function withFakePython(name, answer) {
    const dir = join(work, name);
    mkdirSync(join(dir, 'venv', 'bin'), { recursive: true });
    const python = join(dir, 'venv', 'bin', 'python');
    writeFileSync(python, `#!/bin/sh\ncat <<'JSON'\n${answer}\nJSON\n`);
    chmodSync(python, 0o755);
    return dir;
  }

  it('accepts a runtime whose SDK still carries the fields the client reads', async () => {
    const dir = withFakePython('good', JSON.stringify({ state: 'ok', version: '1.29.1' }));
    await expect(inspectMcpClient(dir)).resolves.toMatchObject({
      ok: true,
      state: 'ok',
      version: '1.29.1',
    });
  });

  it('rejects a runtime whose SDK renamed the tool contract', async () => {
    const dir = withFakePython(
      'renamed',
      JSON.stringify({
        state: 'incompatible',
        version: '2.0.0',
        tool_fields: ['description', 'input_schema', 'name'],
      }),
    );
    const result = await inspectMcpClient(dir);
    expect(result.ok).toBe(false);
    expect(result.reason).toContain('2.0.0');
    expect(result.reason).toContain('input_schema');
    expect(result.reason).toContain('pyproject.toml');
  });

  it('refuses an artifact whose MCP client cannot be asked at all', async () => {
    const dir = join(work, 'broken-python');
    mkdirSync(join(dir, 'venv', 'bin'), { recursive: true });
    const python = join(dir, 'venv', 'bin', 'python');
    writeFileSync(python, '#!/bin/sh\nexit 3\n');
    chmodSync(python, 0o755);
    const result = await inspectMcpClient(dir);
    expect(result.ok).toBe(false);
    expect(result.state).toBe('unprobeable');
  });

  it('passes a runtime with no MCP SDK, reporting it as absent', async () => {
    const dir = withFakePython(
      'no-sdk',
      JSON.stringify({ state: 'absent', detail: "No module named 'mcp'" }),
    );
    await expect(inspectMcpClient(dir)).resolves.toMatchObject({
      ok: true,
      state: 'absent',
    });
  });

  it('says nothing about an artifact that carries no interpreter', async () => {
    const dir = join(work, 'no-python');
    mkdirSync(join(dir, 'bin'), { recursive: true });
    await expect(inspectMcpClient(dir)).resolves.toMatchObject({
      ok: true,
      state: 'no-interpreter',
    });
  });
});

describe('mainCheckoutRoot', () => {
  const gitEnv = {
    ...process.env,
    GIT_CONFIG_GLOBAL: '/dev/null',
    GIT_CONFIG_SYSTEM: '/dev/null',
  };
  const git = (cwd, ...args) =>
    execFileSync('git', ['-c', 'user.name=t', '-c', 'user.email=t@t', ...args], {
      cwd,
      env: gitEnv,
      stdio: ['ignore', 'pipe', 'pipe'],
    });

  it('resolves the main checkout from a linked worktree', () => {
    const main = join(work, 'main-checkout');
    mkdirSync(main, { recursive: true });
    git(main, 'init');
    git(main, 'commit', '--allow-empty', '-m', 'x');
    const worktree = join(main, '.claude', 'worktrees', 'wt');
    git(main, 'worktree', 'add', worktree);

    expect(realpathSync(mainCheckoutRoot(worktree, '/fallback'))).toBe(realpathSync(main));
    expect(realpathSync(mainCheckoutRoot(main, '/fallback'))).toBe(realpathSync(main));
  });

  it('falls back to the static layout outside a git checkout', () => {
    const plain = join(work, 'no-git-here');
    mkdirSync(plain, { recursive: true });
    expect(mainCheckoutRoot(plain, '/fallback')).toBe('/fallback');
  });
});

describe('bundle-agent CLI', () => {
  it('stages an explicit artifact and preserves its symlinks', () => {
    const dir = makeArtifact('hermes-1.2.3-darwin-arm64', '1.2.3');

    const out = runScript({ GOOSAR_HERMES_ARTIFACT: dir });

    expect(out).toMatch(/bundled hermes 1\.2\.3/);
    expect(existsSync(join(stagingDir, 'bin', 'hermes'))).toBe(true);
    const link = join(stagingDir, 'venv', 'bin', 'python');
    expect(lstatSync(link).isSymbolicLink()).toBe(true);
    expect(readlinkSync(link)).toBe('../../python/bin/python3.13');
  });

  it('picks the artifact matching the requested target from a dist dir', () => {
    makeArtifact('hermes-1.0.0-darwin-arm64', '1.0.0');
    makeArtifact('hermes-1.0.0-linux-x86_64', '1.0.0');

    runScript({ GOOSAR_HERMES_DIST_DIR: work }, [
      '--target-platform',
      'linux',
      '--target-arch',
      'x64',
    ]);

    expect(existsSync(join(stagingDir, 'VERSION'))).toBe(true);
    expect(existsSync(join(stagingDir, 'bin', 'hermes'))).toBe(true);
  });

  it('is not fatal when no artifact is available', () => {
    const out = runScript({ GOOSAR_HERMES_DIST_DIR: join(work, 'empty') });

    expect(out).toMatch(/building without the agent runtime/);
    expect(existsSync(stagingDir)).toBe(true);
    expect(existsSync(join(stagingDir, 'bin', 'hermes'))).toBe(false);
    expect(existsSync(join(stagingDir, 'ARTIFACT-NOT-BUNDLED.txt'))).toBe(true);
  });

  it('fails loudly under --require-artifact and names the override', () => {
    expect(() =>
      runScript({ GOOSAR_HERMES_DIST_DIR: join(work, 'empty') }, ['--require-artifact']),
    ).toThrow(/GOOSAR_HERMES_DIST_DIR/);
  });

  it('keeps artifact-less targets buildable under --require-artifact', () => {
    const out = runScript({}, [
      '--target-platform',
      'win32',
      '--target-arch',
      'x64',
      '--require-artifact',
    ]);
    expect(out).toMatch(/no agent runtime artifact exists for win32/);
    expect(existsSync(join(stagingDir, 'ARTIFACT-NOT-BUNDLED.txt'))).toBe(true);
  });

  it('clears a stale payload from a previous target', () => {
    const first = makeArtifact('hermes-1.0.0-darwin-arm64', '1.0.0');
    runScript({ GOOSAR_HERMES_ARTIFACT: first });
    expect(existsSync(join(stagingDir, 'bin', 'hermes'))).toBe(true);

    runScript({ GOOSAR_HERMES_DIST_DIR: join(work, 'empty') });

    expect(existsSync(join(stagingDir, 'bin', 'hermes'))).toBe(false);
  });

  it('refuses a Windows target instead of shipping a POSIX tree', () => {
    const dir = makeArtifact('hermes-1.0.0-darwin-arm64', '1.0.0');
    const out = runScript({ GOOSAR_HERMES_ARTIFACT: dir }, [
      '--target-platform',
      'win32',
      '--target-arch',
      'x64',
    ]);
    expect(out).toMatch(/no agent runtime artifact exists for win32/);
    expect(existsSync(join(stagingDir, 'bin', 'hermes'))).toBe(false);
  });
});

describe('bundle-agent artifact integrity record', () => {
  it('records a sha256 that matches an independent re-hash of the staged artifact', async () => {
    const dir = makeArtifact('hermes-1.2.3-darwin-arm64', '1.2.3');

    const out = runScript({ GOOSAR_HERMES_ARTIFACT: dir });

    const manifest = readManifest(stagingDir);
    expect(manifest.sha256).toMatch(/^[0-9a-f]{64}$/);
    expect(manifest.version).toBe('1.2.3');
    expect(manifest.source).toBe(dir);

    const recomputed = await hashArtifactTree(stagingDir);
    expect(recomputed).toBe(manifest.sha256);

    expect(out).toMatch(new RegExp(`artifact sha256: ${manifest.sha256}`));
  });

  it('produces different hashes for artifacts with different content', async () => {
    const dirA = makeArtifact('hermes-1.0.0-darwin-arm64', '1.0.0');
    runScript({ GOOSAR_HERMES_ARTIFACT: dirA });
    const hashA = readManifest(stagingDir).sha256;

    rmSync(stagingRoot, { recursive: true, force: true });

    const dirB = makeArtifact('hermes-2.0.0-darwin-arm64', '2.0.0');
    runScript({ GOOSAR_HERMES_ARTIFACT: dirB });
    const hashB = readManifest(stagingDir).sha256;

    expect(hashA).not.toBe(hashB);
  });

  it("ships the artifact's licence files and records them in the manifest", () => {
    const dir = makeArtifact('hermes-1.2.3-darwin-arm64', '1.2.3');
    writeFileSync(join(dir, 'LICENSE'), 'Apache License 2.0\n');
    writeFileSync(join(dir, 'NOTICE'), 'notice\n');

    const out = runScript({ GOOSAR_HERMES_ARTIFACT: dir });

    expect(readFileSync(join(stagingDir, 'LICENSE'), 'utf-8')).toBe('Apache License 2.0\n');
    expect(readManifest(stagingDir).licenses).toBe('LICENSE,NOTICE');
    expect(out).toMatch(/artifact licence files: LICENSE, NOTICE/);
  });

  it('warns and records NONE when the artifact carries no licence file', () => {
    const dir = makeArtifact('hermes-1.2.3-darwin-arm64', '1.2.3');

    const out = runScript({ GOOSAR_HERMES_ARTIFACT: dir });

    expect(readManifest(stagingDir).licenses).toBe('NONE');
    expect(out).toMatch(/carries no licence file/);
  });

  it('hashArtifactTree ignores its own manifest file', async () => {
    const dir = makeArtifact('hermes-1.2.3-darwin-arm64', '1.2.3');
    runScript({ GOOSAR_HERMES_ARTIFACT: dir });

    const hashWithManifest = await hashArtifactTree(stagingDir);
    const manifest = readManifest(stagingDir);
    expect(hashWithManifest).toBe(manifest.sha256);
  });
});
