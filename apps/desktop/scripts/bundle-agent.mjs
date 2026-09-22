#!/usr/bin/env node

import { access, mkdir, readFile, readdir, rm, stat, writeFile } from 'node:fs/promises';
import { constants } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

import { hashArtifactTree } from './hash-artifact-tree.mjs';
import {
  MCP_CONTRACT_PROBE,
  classifyMcpProbeAnswer,
  incompatibleMcpClientReason,
} from './mcp-contract.mjs';

export { hashArtifactTree };

const here = dirname(fileURLToPath(import.meta.url));
const desktopRoot = resolve(here, '..');
const repoRoot = resolve(desktopRoot, '..', '..');

export function mainCheckoutRoot(startDir, fallbackRoot) {
  try {
    const commonDir = execFileSync(
      'git',
      ['rev-parse', '--path-format=absolute', '--git-common-dir'],
      { cwd: startDir, encoding: 'utf-8', stdio: ['ignore', 'pipe', 'ignore'] },
    ).trim();
    if (commonDir) return dirname(commonDir);
  } catch {
    console.warn(
      '[bundle-agent] git common-dir resolution failed — falling back to the static repo layout.',
    );
  }
  return fallbackRoot;
}

export const STAGING_DIR = join(desktopRoot, 'resources-agent', 'hermes');

const MISSING_MARKER = 'ARTIFACT-NOT-BUNDLED.txt';

const ARTIFACT_MANIFEST = 'ARTIFACT-MANIFEST.txt';

const LICENSE_FILE_RE = /^(license|licence|notice|copying)(\..*)?$/i;

export async function licenseFilesIn(dir) {
  try {
    const entries = await readdir(dir, { withFileTypes: true });
    return entries
      .filter((entry) => entry.isFile() && LICENSE_FILE_RE.test(entry.name))
      .map((entry) => entry.name)
      .sort();
  } catch {
    return [];
  }
}

const PLATFORM_TO_UNAME_OS = {
  darwin: 'darwin',
  linux: 'linux',
};

const ARCH_TO_UNAME_MACHINES = {
  x64: ['x86_64', 'amd64'],
  arm64: ['arm64', 'aarch64'],
};

function argValue(argv, flag) {
  const index = argv.indexOf(flag);
  if (index === -1) return null;
  return argv[index + 1] ?? '';
}

async function exists(path) {
  try {
    await access(path, constants.F_OK);
    return true;
  } catch {
    return false;
  }
}

async function isExecutable(path) {
  try {
    await access(path, constants.X_OK);
    return true;
  } catch {
    return false;
  }
}

export function artifactNameMatches(name, targetPlatform, targetArch) {
  const os = PLATFORM_TO_UNAME_OS[targetPlatform];
  const machines = ARCH_TO_UNAME_MACHINES[targetArch];
  if (!os || !machines) return false;
  return machines.some(
    (machine) => name.startsWith('hermes-') && name.endsWith(`-${os}-${machine}`),
  );
}

export async function inspectMcpClient(dir) {
  const python = join(dir, 'venv', 'bin', 'python');
  if (!(await isExecutable(python))) {
    return { ok: true, state: 'no-interpreter' };
  }
  let raw;
  try {
    raw = execFileSync(python, ['-c', MCP_CONTRACT_PROBE], {
      encoding: 'utf-8',
      stdio: ['ignore', 'pipe', 'ignore'],
      timeout: 60_000,
    });
  } catch (err) {
    return {
      ok: false,
      state: 'unprobeable',
      reason: `could not ask ${python} about its MCP client: ${err.message ?? err}`,
    };
  }
  let answer;
  try {
    answer = JSON.parse(raw.trim().split('\n').pop() ?? '');
  } catch {
    return {
      ok: false,
      state: 'unprobeable',
      reason: `unreadable answer from the MCP client probe: ${raw.trim()}`,
    };
  }
  const verdict = classifyMcpProbeAnswer(answer);
  if (verdict.state === 'incompatible') {
    return {
      ...verdict,
      reason:
        `${incompatibleMcpClientReason(answer)} Pin mcp <2 in the agent's ` +
        'pyproject.toml and rebuild the artifact.',
    };
  }
  return verdict;
}

export async function inspectArtifact(dir) {
  const bin = join(dir, 'bin', 'hermes');
  const versionFile = join(dir, 'VERSION');
  const installer = join(dir, 'bin', 'install-artifact.sh');

  if (!(await exists(dir))) return { ok: false, reason: `no such directory: ${dir}` };
  if (!(await isExecutable(bin))) {
    return { ok: false, reason: `missing executable bin/hermes in ${dir}` };
  }
  if (!(await exists(versionFile))) {
    return { ok: false, reason: `missing VERSION in ${dir}` };
  }
  if (!(await exists(installer))) {
    return {
      ok: false,
      reason:
        `missing bin/install-artifact.sh in ${dir} — the artifact must carry ` +
        'its own installer (hermes-agent scripts/install-artifact.sh)',
    };
  }
  const version = (await readFile(versionFile, 'utf-8')).trim();
  if (!version) return { ok: false, reason: `empty VERSION in ${dir}` };

  const mcpClient = await inspectMcpClient(dir);
  if (!mcpClient.ok) return { ok: false, reason: mcpClient.reason };
  if (mcpClient.state === 'absent') {
    console.warn(
      '[bundle-agent] WARNING: the artifact carries no `mcp` SDK, so this ' +
        'build ships with no MCP integrations at all ' +
        `(${mcpClient.detail}).`,
    );
  }
  return { ok: true, version, mcpClient };
}

async function findArtifactInDist(distDir, targetPlatform, targetArch) {
  let entries;
  try {
    entries = await readdir(distDir, { withFileTypes: true });
  } catch {
    return null;
  }
  const names = entries
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name)
    .filter((name) => artifactNameMatches(name, targetPlatform, targetArch));

  const stats = await Promise.all(
    names.map(async (name) => {
      const dir = join(distDir, name);
      try {
        return { dir, mtimeMs: (await stat(dir)).mtimeMs };
      } catch {
        return null;
      }
    }),
  );
  const newest = stats
    .filter((entry) => entry !== null)
    .sort((a, b) => a.mtimeMs - b.mtimeMs)
    .at(-1);
  return newest ? newest.dir : null;
}

async function resolveArtifactDir(targetPlatform, targetArch) {
  const explicit = process.env['GOOSAR_HERMES_ARTIFACT']?.trim();
  if (explicit) return { dir: resolve(explicit), source: 'GOOSAR_HERMES_ARTIFACT' };

  const distDir = process.env['GOOSAR_HERMES_DIST_DIR']?.trim();
  if (distDir) {
    const found = await findArtifactInDist(resolve(distDir), targetPlatform, targetArch);
    return { dir: found, source: 'GOOSAR_HERMES_DIST_DIR' };
  }

  const sibling = resolve(mainCheckoutRoot(desktopRoot, repoRoot), '..', 'hermes-agent', 'dist');
  const found = await findArtifactInDist(sibling, targetPlatform, targetArch);
  return { dir: found, source: sibling };
}

function copyTree(src, dest) {
  const flags = process.platform === 'darwin' ? ['-Rc'] : ['-R'];
  execFileSync('cp', [...flags, src, dest], { stdio: 'inherit' });
}

async function writeMissingMarker(reason) {
  await rm(STAGING_DIR, { recursive: true, force: true });
  await mkdir(STAGING_DIR, { recursive: true });
  await writeFile(
    join(STAGING_DIR, MISSING_MARKER),
    `No hermes agent runtime artifact was bundled into this build.\n\n` +
      `Reason: ${reason}\n\n` +
      `Build one with hermes-agent's scripts/build-artifact.sh and point\n` +
      `GOOSAR_HERMES_ARTIFACT at it (or drop it in ../hermes-agent/dist).\n` +
      `The app treats this as "agent runtime not installed" at runtime.\n`,
    'utf-8',
  );
}

async function writeArtifactManifest(
  stagingDir,
  { version, source, platform, arch, sha256, licenses },
) {
  await writeFile(
    join(stagingDir, ARTIFACT_MANIFEST),
    `hermes agent runtime artifact staged into this build.\n\n` +
      `version=${version}\n` +
      `licenses=${licenses.length > 0 ? licenses.join(',') : 'NONE'}\n` +
      `source=${source}\n` +
      `platform=${platform}\n` +
      `arch=${arch}\n` +
      `sha256=${sha256}\n` +
      `staged_at=${new Date().toISOString()}\n\n` +
      `Verify by re-running hashArtifactTree() (apps/desktop/scripts/bundle-agent.mjs)\n` +
      `against this directory and comparing the result to sha256 above.\n`,
    'utf-8',
  );
}

async function main() {
  const argv = process.argv.slice(2);
  const targetPlatform = argValue(argv, '--target-platform') ?? process.platform;
  const targetArch = argValue(argv, '--target-arch') ?? process.arch;
  const requireArtifact = argv.includes('--require-artifact');

  const skipWithMarker = async (reason) => {
    console.warn(`[bundle-agent] ${reason} — building without the agent runtime.`);
    await writeMissingMarker(reason);
    process.exit(0);
  };

  const fail = async (reason) => {
    if (requireArtifact) {
      console.error(
        `[bundle-agent] ${reason}\n` +
          `[bundle-agent] refusing to build: the packaged app would ship WITHOUT the agent runtime,\n` +
          `[bundle-agent] and an incomplete build is worse than a failed one (#84).\n` +
          `[bundle-agent] Build an artifact with hermes-agent's scripts/build-artifact.sh, then point\n` +
          `[bundle-agent] GOOSAR_HERMES_DIST_DIR at the dist directory that holds it\n` +
          `[bundle-agent] (or GOOSAR_HERMES_ARTIFACT at one artifact directory).`,
      );
      process.exit(1);
    }
    await skipWithMarker(reason);
  };

  if (!PLATFORM_TO_UNAME_OS[targetPlatform]) {
    await skipWithMarker(
      `no agent runtime artifact exists for ${targetPlatform} ` + '(ADanMan/hermes-agent#38)',
    );
    return;
  }

  const { dir, source } = await resolveArtifactDir(targetPlatform, targetArch);
  if (!dir) {
    await fail(`no hermes artifact for ${targetPlatform}/${targetArch} found in ${source}`);
    return;
  }

  const inspected = await inspectArtifact(dir);
  if (!inspected.ok) {
    await fail(`unusable artifact from ${source}: ${inspected.reason}`);
    return;
  }

  await rm(join(desktopRoot, 'resources-agent'), { recursive: true, force: true });
  await mkdir(dirname(STAGING_DIR), { recursive: true });
  console.log(
    `[bundle-agent] staging hermes ${inspected.version} (${targetPlatform}/${targetArch})\n` +
      `[bundle-agent]   from ${dir}\n` +
      `[bundle-agent]   to   ${STAGING_DIR}`,
  );
  copyTree(dir, STAGING_DIR);

  const staged = await inspectArtifact(STAGING_DIR);
  if (!staged.ok) {
    console.error(`[bundle-agent] staged copy is unusable: ${staged.reason}`);
    process.exit(1);
  }

  const sha256 = await hashArtifactTree(STAGING_DIR);
  const licenses = await licenseFilesIn(STAGING_DIR);
  if (licenses.length === 0) {
    console.warn(
      '[bundle-agent] WARNING: the agent runtime artifact carries no licence file, ' +
        "so the DMG's largest component ships with no terms " +
        '(docs/compliance/agent-runtime-license.md).',
    );
  } else {
    console.log(`[bundle-agent] artifact licence files: ${licenses.join(', ')}`);
  }
  await writeArtifactManifest(STAGING_DIR, {
    version: staged.version,
    source: dir,
    platform: targetPlatform,
    arch: targetArch,
    sha256,
    licenses,
  });
  console.log(`[bundle-agent] bundled hermes ${staged.version}`);
  console.log(`[bundle-agent] artifact sha256: ${sha256}`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  await main();
}
