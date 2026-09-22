#!/usr/bin/env node

import { execFileSync, spawnSync } from 'node:child_process';
import { existsSync, readdirSync, rmSync } from 'node:fs';
import { basename, delimiter, dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

import {
  SOURCE_ENV as MCP_SERVERS_SOURCE_ENV,
  STAGING_DIR as MCP_SERVERS_STAGING_DIR,
} from './bundle-mcp-servers.mjs';
import {
  PLAYWRIGHT_SOURCE_ENV,
  PLAYWRIGHT_STAGING_DIR,
  SKILLS_SOURCE_ENV,
  SKILLS_STAGING_DIR,
} from './bundle-skills.mjs';
import { SOURCE_ENV as PRESET_OVERLAY_SOURCE_ENV } from './bundle-preset-overlay.mjs';
import { SOURCE_ENV as DEPLOYMENT_DEFAULTS_SOURCE_ENV } from './bundle-deployment-defaults.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const desktopRoot = resolve(here, '..');
const bundleCliScript = resolve(here, 'bundle-cli.mjs');
const verifyPackagedArtifactScript = resolve(here, 'verify-packaged-artifact.mjs');
const buildMacDmgScript = resolve(here, 'build-mac-dmg.mjs');
const bundleAgentScript = resolve(here, 'bundle-agent.mjs');
const bundleCaScript = resolve(here, 'bundle-ca.mjs');
const bundleMcpServersScript = resolve(here, 'bundle-mcp-servers.mjs');
const bundleSkillsScript = resolve(here, 'bundle-skills.mjs');
const bundlePresetOverlayScript = resolve(here, 'bundle-preset-overlay.mjs');
const bundleDeploymentDefaultsScript = resolve(here, 'bundle-deployment-defaults.mjs');
const verifyMcpServerScript = resolve(here, 'verify-mcp-server.mjs');
const verifySkillScript = resolve(here, 'verify-skill.mjs');

const PLATFORM_CONFIG = {
  mac: {
    aliases: new Set(['--mac', '--macos', '-m']),
    builderFlag: '--mac',
    runtimePlatform: 'darwin',
    label: 'macOS',
  },
  win: {
    aliases: new Set(['--win', '--windows', '-w']),
    builderFlag: '--win',
    runtimePlatform: 'win32',
    label: 'Windows',
  },
  linux: {
    aliases: new Set(['--linux', '-l']),
    builderFlag: '--linux',
    runtimePlatform: 'linux',
    label: 'Linux',
  },
};

const ARCH_FLAGS = new Map([
  ['--x64', 'x64'],
  ['--arm64', 'arm64'],
  ['--ia32', 'ia32'],
  ['--armv7l', 'armv7l'],
  ['--universal', 'universal'],
]);

const SUPPORTED_CLI_ARCHS = new Set(['x64', 'arm64']);
const MAC_ALL_PLATFORM_TARGETS = [
  { platform: 'mac', arch: 'arm64' },
  { platform: 'mac', arch: 'x64' },
  { platform: 'win', arch: 'x64' },
  { platform: 'win', arch: 'arm64' },
  { platform: 'linux', arch: 'x64' },
  { platform: 'linux', arch: 'arm64' },
];

function git(args, cwd) {
  try {
    return execFileSync('git', args, { encoding: 'utf-8', cwd }).trim();
  } catch {
    return '';
  }
}

export function stripLeadingSeparator(argv) {
  if (argv.length > 0 && argv[0] === '--') return argv.slice(1);
  return argv;
}

export function normalizeGitVersion(raw) {
  if (!raw) return null;
  const stripped = raw.replace(/^v/, '');
  if (!/^\d+\.\d+\.\d+/.test(stripped)) {
    return `0.0.0-g${stripped}`;
  }
  return stripped;
}

export const DESCRIBE_ARGS = ['describe', '--tags', '--match', 'v[0-9]*', '--always', '--dirty'];

export function deriveVersion(cwd) {
  return normalizeGitVersion(git(DESCRIBE_ARGS, cwd));
}

function uniqueOrdered(values) {
  return [...new Set(values)];
}

export function envWithLocalBins(env = process.env, root = desktopRoot) {
  const pathKey = Object.keys(env).find((key) => key.toUpperCase() === 'PATH') ?? 'PATH';
  const existingPath = env[pathKey] ?? '';
  const localBins = uniqueOrdered([
    resolve(root, 'node_modules', '.bin'),
    resolve(root, '..', '..', 'node_modules', '.bin'),
  ]);
  const mergedPath = uniqueOrdered([
    ...localBins,
    ...String(existingPath).split(delimiter).filter(Boolean),
  ]).join(delimiter);
  return { ...env, [pathKey]: mergedPath };
}

export function bundlingEnv(thin, env = process.env, root = desktopRoot) {
  const base = envWithLocalBins(env, root);
  if (!thin) return base;
  const stripped = { ...base };
  delete stripped[MCP_SERVERS_SOURCE_ENV];
  delete stripped[SKILLS_SOURCE_ENV];
  delete stripped[PLAYWRIGHT_SOURCE_ENV];
  delete stripped[PRESET_OVERLAY_SOURCE_ENV];
  delete stripped[DEPLOYMENT_DEFAULTS_SOURCE_ENV];
  return stripped;
}

function hostPlatformKey(platform = process.platform) {
  if (platform === 'darwin') return 'mac';
  if (platform === 'win32') return 'win';
  if (platform === 'linux') return 'linux';
  throw new Error(`[package] unsupported host platform: ${platform}`);
}

function hostArchKey(arch = process.arch) {
  if (SUPPORTED_CLI_ARCHS.has(arch)) return arch;
  throw new Error(`[package] unsupported host architecture for Desktop CLI bundling: ${arch}`);
}

function expandPlatformShorthand(token) {
  if (!/^-[mwl]{2,}$/.test(token)) return null;
  const expanded = [];
  for (const char of token.slice(1)) {
    if (char === 'm') expanded.push('mac');
    if (char === 'w') expanded.push('win');
    if (char === 'l') expanded.push('linux');
  }
  return uniqueOrdered(expanded);
}

function platformKeyForToken(token) {
  for (const [platform, config] of Object.entries(PLATFORM_CONFIG)) {
    if (config.aliases.has(token)) return platform;
  }
  return null;
}

function platformTargetsTemplate() {
  return { mac: [], win: [], linux: [] };
}

export function parsePackageArgs(argv) {
  const sharedArgs = [];
  const platformTargets = platformTargetsTemplate();
  const requestedPlatforms = [];
  const requestedArchs = [];
  let allPlatforms = false;
  let requireMcpServers = false;
  let requireSkills = false;
  let thin = false;

  for (let i = 0; i < argv.length; i += 1) {
    const token = argv[i];
    if (token === '--all-platforms') {
      allPlatforms = true;
      continue;
    }
    if (token === '--require-mcp-servers') {
      requireMcpServers = true;
      continue;
    }
    if (token === '--require-skills') {
      requireSkills = true;
      continue;
    }
    if (token === '--thin') {
      thin = true;
      continue;
    }

    const expandedPlatforms = expandPlatformShorthand(token);
    if (expandedPlatforms) {
      requestedPlatforms.push(...expandedPlatforms);
      continue;
    }

    const platform = platformKeyForToken(token);
    if (platform) {
      requestedPlatforms.push(platform);
      while (i + 1 < argv.length && !argv[i + 1].startsWith('-')) {
        platformTargets[platform].push(argv[i + 1]);
        i += 1;
      }
      continue;
    }

    const arch = ARCH_FLAGS.get(token);
    if (arch) {
      requestedArchs.push(arch);
      continue;
    }

    sharedArgs.push(token);
  }

  if (thin && (requireMcpServers || requireSkills)) {
    throw new Error(
      '[package] --thin cannot be combined with --require-mcp-servers / --require-skills ' +
        '(thin explicitly omits that payload; require explicitly demands it)',
    );
  }

  return {
    allPlatforms,
    sharedArgs,
    platformTargets,
    requestedPlatforms: uniqueOrdered(requestedPlatforms),
    requestedArchs: uniqueOrdered(requestedArchs),
    requireMcpServers,
    requireSkills,
    thin,
  };
}

export function resolveBuildMatrix(parsed, platform = process.platform, arch = process.arch) {
  if (parsed.allPlatforms) {
    if (parsed.requestedPlatforms.length > 0 || parsed.requestedArchs.length > 0) {
      throw new Error(
        '[package] --all-platforms cannot be combined with explicit platform or arch flags',
      );
    }
    if (platform !== 'darwin') {
      throw new Error(
        `[package] --all-platforms is only supported on macOS hosts (current: ${platform})`,
      );
    }
    return MAC_ALL_PLATFORM_TARGETS.map((target) => ({ ...target }));
  }

  const platforms =
    parsed.requestedPlatforms.length > 0 ? parsed.requestedPlatforms : [hostPlatformKey(platform)];
  const archs = parsed.requestedArchs.length > 0 ? parsed.requestedArchs : [hostArchKey(arch)];

  const unsupported = archs.filter((value) => !SUPPORTED_CLI_ARCHS.has(value));
  if (unsupported.length > 0) {
    throw new Error(
      `[package] unsupported Desktop CLI architecture(s): ${unsupported.join(', ')}. ` +
        'Use --x64 or --arm64.',
    );
  }

  return platforms.flatMap((targetPlatform) =>
    archs.map((targetArch) => ({
      platform: targetPlatform,
      arch: targetArch,
    })),
  );
}

function formatTarget(target) {
  return `${PLATFORM_CONFIG[target.platform].label} ${target.arch}`;
}

export function builderArgsForTarget(
  target,
  parsed,
  version,
  { disableMacNotarize = false, hostPlatform = process.platform, useScopedOutputDir = false } = {},
) {
  const builderArgs = [];
  if (version) builderArgs.push(`-c.extraMetadata.version=${version}`);
  if (disableMacNotarize) builderArgs.push('-c.mac.notarize=false');
  builderArgs.push(PLATFORM_CONFIG[target.platform].builderFlag);
  const requestedTargets = parsed.platformTargets[target.platform];
  if (target.platform === 'linux' && hostPlatform !== 'linux' && requestedTargets.length === 0) {
    builderArgs.push('AppImage');
  } else {
    builderArgs.push(...requestedTargets);
  }
  builderArgs.push(`--${target.arch}`);
  builderArgs.push(...parsed.sharedArgs);
  if (useScopedOutputDir) {
    builderArgs.push(`-c.directories.output=dist/${target.platform}-${target.arch}`);
  }
  if (target.platform === 'win' && target.arch === 'arm64') {
    builderArgs.push('-c.publish.channel=latest-arm64');
  }
  if (target.platform === 'mac' && target.arch === 'x64') {
    builderArgs.push('-c.mac.minimumSystemVersion=12.0.0');
    builderArgs.push('-c.publish.channel=latest-x64');
  }
  return builderArgs;
}

function runBundleVerifier(label, script, args) {
  const result = spawnSync('node', [script, ...args], {
    stdio: 'inherit',
    cwd: desktopRoot,
    env: envWithLocalBins(),
  });
  if (result.error) {
    console.error(`[package] failed to spawn ${label}:`, result.error.message);
    process.exit(1);
  }
  if (result.status !== 0) {
    console.error(
      `[package] ${label} FAILED — refusing to package a bundle whose ` +
        'staged servers/skills did not come up.',
    );
    process.exit(result.status ?? 1);
  }
}

export function findMacBuildArtifacts({ distRoot, platform, arch, scoped }) {
  const searchRoot = scoped ? join(distRoot, `${platform}-${arch}`) : distRoot;

  const appPath = findAppBundle(searchRoot, 3);
  if (!appPath) {
    return { appPath: null, zipPath: null, dmgPath: null, searchRoot };
  }
  const zips = findFiles(searchRoot, 3, (name) => name.endsWith('.zip'));
  const zipPath = zips.find((p) => basename(p).endsWith(`-mac-${arch}.zip`)) ?? zips[0] ?? null;
  const dmgName = zipPath
    ? basename(zipPath).replace(/\.zip$/, '.dmg')
    : `${basename(appPath, '.app')}-mac-${arch}.dmg`;
  return { appPath, zipPath, dmgPath: join(distRoot, dmgName), searchRoot };
}

function findAppBundle(dir, depth) {
  let entries;
  try {
    entries = readdirSync(dir, { withFileTypes: true });
  } catch {
    return null; 
  }
  for (const entry of entries) {
    if (!entry.isDirectory()) continue;
    if (entry.name.endsWith('.app')) return join(dir, entry.name);
  }
  if (depth <= 1) return null;
  for (const entry of entries) {
    if (!entry.isDirectory() || entry.name.endsWith('.app')) continue;
    const found = findAppBundle(join(dir, entry.name), depth - 1);
    if (found) return found;
  }
  return null;
}

function findFiles(dir, depth, predicate) {
  let entries;
  try {
    entries = readdirSync(dir, { withFileTypes: true });
  } catch {
    return [];
  }
  const found = [];
  for (const entry of entries) {
    if (entry.isFile() && predicate(entry.name)) found.push(join(dir, entry.name));
  }
  if (depth > 1) {
    for (const entry of entries) {
      if (!entry.isDirectory() || entry.name.endsWith('.app')) continue;
      found.push(...findFiles(join(dir, entry.name), depth - 1, predicate));
    }
  }
  return found;
}

function packageAndVerifyMacArtifacts(target, scoped) {
  const distRoot = resolve(desktopRoot, 'dist');
  const { appPath, zipPath, dmgPath, searchRoot } = findMacBuildArtifacts({
    distRoot,
    platform: target.platform,
    arch: target.arch,
    scoped,
  });
  if (!appPath) {
    console.error(`[package] no .app found under ${searchRoot} — cannot verify mac artifacts.`);
    process.exit(1);
  }

  if (zipPath) {
    runBundleVerifier('verify-artifact(zip)', verifyPackagedArtifactScript, [
      '--source',
      appPath,
      '--zip',
      zipPath,
    ]);
  } else {
    console.warn(`[package] no .zip under ${searchRoot} (skipping zip check).`);
  }

  runBundleVerifier('build-mac-dmg', buildMacDmgScript, [
    '--app',
    appPath,
    '--out',
    dmgPath,
    '--volname',
    basename(appPath, '.app'),
  ]);
  runBundleVerifier('verify-artifact(dmg)', verifyPackagedArtifactScript, [
    '--source',
    appPath,
    '--dmg',
    dmgPath,
  ]);
}

function main() {
  const passthrough = stripLeadingSeparator(process.argv.slice(2));
  const parsed = parsePackageArgs(passthrough);
  const buildMatrix = resolveBuildMatrix(parsed);
  console.log(`[package] build matrix → ${buildMatrix.map(formatTarget).join(', ')}`);
  console.log(
    parsed.thin
      ? '[package] build profile → thin (skills/MCP servers/shared Chromium omitted; client provisions on first launch — issue #172)'
      : "[package] build profile → fat (whatever the build machine's GOOSAR_MCP_SERVERS_DIR/GOOSAR_SKILLS_DIR/GOOSAR_PLAYWRIGHT_BROWSERS_DIR resolve to; pass --thin for the on-prem default)",
  );

  const distDir = resolve(desktopRoot, 'dist');
  rmSync(distDir, { recursive: true, force: true });
  console.log(`[package] cleaned output dir → ${distDir}`);

  const viteResult = spawnSync('electron-vite', ['build'], {
    stdio: 'inherit',
    cwd: desktopRoot,
    env: envWithLocalBins(),
    shell: true,
  });
  if (viteResult.error) {
    console.error('[package] failed to spawn electron-vite:', viteResult.error.message);
    process.exit(1);
  }
  if (viteResult.status !== 0) {
    process.exit(viteResult.status ?? 1);
  }

  const version = deriveVersion();
  if (version) {
    console.log(`[package] Desktop version → ${version} (from git describe)`);
  } else {
    console.warn('[package] could not derive version from git; falling back to package.json');
  }

  const disableMacNotarize = !process.env.APPLE_TEAM_ID;
  if (disableMacNotarize) {
    console.warn(
      '[package] APPLE_TEAM_ID not set — skipping notarization (local dev build). ' +
        'Set APPLE_ID + APPLE_APP_SPECIFIC_PASSWORD + APPLE_TEAM_ID for a release build.',
    );
  }

  const useScopedOutputDir = buildMatrix.length > 1;

  for (const target of buildMatrix) {
    const targetArgs = [
      '--target-platform',
      PLATFORM_CONFIG[target.platform].runtimePlatform,
      '--target-arch',
      target.arch,
    ];
    console.log(`[package] bundling CLI → ${formatTarget(target)}`);
    execFileSync('node', [bundleCliScript, ...targetArgs], {
      stdio: 'inherit',
      cwd: desktopRoot,
    });

    const requireAgentArgs = target.platform === 'mac' ? ['--require-artifact'] : [];
    console.log(`[package] bundling agent runtime → ${formatTarget(target)}`);
    execFileSync('node', [bundleAgentScript, ...targetArgs, ...requireAgentArgs], {
      stdio: 'inherit',
      cwd: desktopRoot,
    });

    console.log(`[package] bundling corporate CA → ${formatTarget(target)}`);
    execFileSync('node', [bundleCaScript], {
      stdio: 'inherit',
      cwd: desktopRoot,
    });

    console.log(
      `[package] bundling MCP servers → ${formatTarget(target)}${parsed.thin ? ' (--thin: source env withheld)' : ''}`,
    );
    const requireMcpArgs = parsed.requireMcpServers ? ['--require-mcp-servers'] : [];
    execFileSync('node', [bundleMcpServersScript, ...requireMcpArgs], {
      stdio: 'inherit',
      cwd: desktopRoot,
      env: bundlingEnv(parsed.thin),
    });

    console.log(
      `[package] bundling skills → ${formatTarget(target)}${parsed.thin ? ' (--thin: source env withheld)' : ''}`,
    );
    const requireSkillsArgs = parsed.requireSkills ? ['--require-skills'] : [];
    execFileSync('node', [bundleSkillsScript, ...requireSkillsArgs], {
      stdio: 'inherit',
      cwd: desktopRoot,
      env: bundlingEnv(parsed.thin),
    });

    console.log(
      `[package] bundling preset overlay → ${formatTarget(target)}${parsed.thin ? ' (--thin: source env withheld)' : ''}`,
    );
    execFileSync('node', [bundlePresetOverlayScript], {
      stdio: 'inherit',
      cwd: desktopRoot,
      env: bundlingEnv(parsed.thin),
    });

    console.log(
      `[package] bundling deployment defaults → ${formatTarget(target)}${parsed.thin ? ' (--thin: source env withheld)' : ''}`,
    );
    execFileSync('node', [bundleDeploymentDefaultsScript], {
      stdio: 'inherit',
      cwd: desktopRoot,
      env: bundlingEnv(parsed.thin),
    });

    console.log(`[package] verifying staged MCP servers → ${formatTarget(target)}`);
    runBundleVerifier('verify-mcp-server', verifyMcpServerScript, [
      '--all',
      MCP_SERVERS_STAGING_DIR,
    ]);
    console.log(`[package] verifying staged skills → ${formatTarget(target)}`);
    runBundleVerifier('verify-skill', verifySkillScript, [
      '--all',
      SKILLS_STAGING_DIR,
      '--browsers',
      PLAYWRIGHT_STAGING_DIR,
    ]);

    const builderArgs = builderArgsForTarget(target, parsed, version, {
      disableMacNotarize,
      hostPlatform: process.platform,
      useScopedOutputDir,
    });

    const result = spawnSync('electron-builder', builderArgs, {
      stdio: 'inherit',
      cwd: desktopRoot,
      env: envWithLocalBins(),
      shell: true,
    });

    if (result.error) {
      console.error('[package] failed to spawn electron-builder:', result.error.message);
      process.exit(1);
    }
    if (result.status !== 0) {
      process.exit(result.status ?? 1);
    }

    if (target.platform === 'mac') {
      packageAndVerifyMacArtifacts(target, useScopedOutputDir);
    }
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}
