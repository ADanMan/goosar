#!/usr/bin/env node

import { access, mkdir, readdir, rm, stat, writeFile } from 'node:fs/promises';
import { constants, createReadStream } from 'node:fs';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

import { hashArtifactTree } from './bundle-agent.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const desktopRoot = resolve(here, '..');

export const SKILLS_STAGING_DIR = join(desktopRoot, 'resources-skills');
export const PLAYWRIGHT_STAGING_DIR = join(desktopRoot, 'resources-playwright');

const SKILLS_MISSING_MARKER = 'SKILLS-NOT-BUNDLED.txt';
const PLAYWRIGHT_MISSING_MARKER = 'PLAYWRIGHT-NOT-BUNDLED.txt';
export const SKILLS_MANIFEST = 'SKILLS-MANIFEST.txt';
export const PLAYWRIGHT_MANIFEST = 'PLAYWRIGHT-MANIFEST.txt';

export const SKILLS_SOURCE_ENV = 'GOOSAR_SKILLS_DIR';
export const PLAYWRIGHT_SOURCE_ENV = 'GOOSAR_PLAYWRIGHT_BROWSERS_DIR';

const SKILL_MANIFEST_FILE = 'SKILL.md';

async function exists(path) {
  try {
    await access(path, constants.F_OK);
    return true;
  } catch {
    return false;
  }
}

function copyTree(src, dest) {
  const flags = process.platform === 'darwin' ? ['-Rc'] : ['-R'];
  execFileSync('cp', [...flags, src, dest], { stdio: 'inherit' });
}

async function listChildDirs(dir) {
  const entries = await readdir(dir, { withFileTypes: true });
  const names = [];
  const skipped = [];
  for (const entry of entries) {
    if (entry.name.startsWith('.')) continue;
    const full = join(dir, entry.name);
    let stats;
    try {
      stats = await stat(full);
    } catch (err) {
      const reason = err?.code === 'ENOENT' ? 'broken symlink' : (err?.message ?? String(err));
      console.warn(`[bundle-skills] WARN: skipped ${entry.name}: ${reason}`);
      skipped.push({ name: entry.name, reason });
      continue;
    }
    if (!stats.isDirectory()) {
      console.warn(`[bundle-skills] WARN: skipped ${entry.name}: not a directory`);
      skipped.push({ name: entry.name, reason: 'not a directory' });
      continue;
    }
    names.push(entry.name);
  }
  return { names: names.sort(), skipped };
}

async function writeMissingMarker(stagingDir, markerName, sourceEnv, kind) {
  await rm(stagingDir, { recursive: true, force: true });
  await mkdir(stagingDir, { recursive: true });
  await writeFile(
    join(stagingDir, markerName),
    `No ${kind} were bundled into this build.\n\n` +
      `Point ${sourceEnv} at the source directory to stage them. Corporate\n` +
      'content is injected this way at build time from the internal\n' +
      'provisioning bundle and is never committed to this public repository.\n',
    'utf-8',
  );
}

async function writeManifest(stagingDir, manifestName, entries, { source, kind }) {
  const lines = [
    `${kind} staged into this build.`,
    '',
    `source=${source}`,
    `count=${entries.length}`,
    `staged_at=${new Date().toISOString()}`,
    '',
    'Per entry: name, sha256 (hashArtifactTree over the staged tree).',
    '',
  ];
  for (const entry of entries) {
    lines.push(`entry=${entry.name} sha256=${entry.sha256}`);
  }
  lines.push('');
  await writeFile(join(stagingDir, manifestName), lines.join('\n'), 'utf-8');
}

export async function stageSkills(sourceDir, { requireNoSkips = false } = {}) {
  await rm(SKILLS_STAGING_DIR, { recursive: true, force: true });
  await mkdir(SKILLS_STAGING_DIR, { recursive: true });

  const { names, skipped } = await listChildDirs(sourceDir);
  if (requireNoSkips && skipped.length > 0) {
    const detail = skipped.map((s) => `${s.name} (${s.reason})`).join(', ');
    throw new Error(
      `--require-skills was passed but ${skipped.length} source ` +
        `entr${skipped.length === 1 ? 'y was' : 'ies were'} skipped: ${detail}`,
    );
  }
  const staged = [];
  for (const name of names) {
    const from = join(sourceDir, name);
    const to = join(SKILLS_STAGING_DIR, name);
    console.log(`[bundle-skills] staging skill ${name}\n  from ${from}\n  to   ${to}`);
    copyTree(from, to);
    if (!(await exists(join(to, SKILL_MANIFEST_FILE)))) {
      console.warn(
        `[bundle-skills] ${name} has no ${SKILL_MANIFEST_FILE} — staging it anyway, ` +
          'but it will fail verification.',
      );
    }
    const sha256 = await hashArtifactTree(to);
    staged.push({ name, sha256 });
  }

  await writeManifest(SKILLS_STAGING_DIR, SKILLS_MANIFEST, staged, {
    source: sourceDir,
    kind: 'Skills',
  });
  return staged;
}

async function listChildEntries(dir) {
  const entries = await readdir(dir, { withFileTypes: true });
  return entries
    .filter((entry) => !entry.name.startsWith('.') && (entry.isDirectory() || entry.isFile()))
    .map((entry) => ({ name: entry.name, isDirectory: entry.isDirectory() }))
    .sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
}

function sha256File(path) {
  return new Promise((resolvePromise, reject) => {
    const hash = createHash('sha256');
    const stream = createReadStream(path);
    stream.on('data', (chunk) => hash.update(chunk));
    stream.on('end', () => resolvePromise(hash.digest('hex')));
    stream.on('error', reject);
  });
}

export async function stagePlaywrightBrowsers(sourceDir) {
  await rm(PLAYWRIGHT_STAGING_DIR, { recursive: true, force: true });
  await mkdir(PLAYWRIGHT_STAGING_DIR, { recursive: true });

  const entries = await listChildEntries(sourceDir);
  const staged = [];
  for (const { name, isDirectory } of entries) {
    const from = join(sourceDir, name);
    const to = join(PLAYWRIGHT_STAGING_DIR, name);
    console.log(
      `[bundle-skills] staging ${isDirectory ? 'browser' : 'file'} ${name}\n  from ${from}\n  to   ${to}`,
    );
    copyTree(from, to);
    const sha256 = isDirectory ? await hashArtifactTree(to) : await sha256File(to);
    staged.push({ name, sha256 });
  }

  await writeManifest(PLAYWRIGHT_STAGING_DIR, PLAYWRIGHT_MANIFEST, staged, {
    source: sourceDir,
    kind: 'Playwright browsers',
  });
  return staged;
}

async function resolveSource(sourceEnv, { requireIt, requireFlag }) {
  const raw = process.env[sourceEnv]?.trim();
  if (!raw) {
    if (requireIt) {
      throw new Error(
        `${sourceEnv} is unset but ${requireFlag} was passed — refusing to ` +
          'build without content this release is expected to carry.',
      );
    }
    return { skip: true };
  }
  const resolved = resolve(raw);
  if (!(await exists(resolved))) {
    throw new Error(
      `${sourceEnv}=${resolved} does not exist. An explicit source directory ` +
        `that is not present is a build mistake; unset ${sourceEnv} to build ` +
        'without it on purpose.',
    );
  }
  return { dir: resolved };
}

async function main() {
  const argv = process.argv.slice(2);
  const requireSkills = argv.includes('--require-skills');

  try {
    const source = await resolveSource(SKILLS_SOURCE_ENV, {
      requireIt: requireSkills,
      requireFlag: '--require-skills',
    });
    if (source.skip) {
      console.warn(`[bundle-skills] ${SKILLS_SOURCE_ENV} is unset — building without skills.`);
      await writeMissingMarker(
        SKILLS_STAGING_DIR,
        SKILLS_MISSING_MARKER,
        SKILLS_SOURCE_ENV,
        'skills',
      );
    } else {
      const skills = await stageSkills(source.dir, { requireNoSkips: requireSkills });
      if (skills.length === 0) {
        console.warn(`[bundle-skills] ${source.dir} holds no skill directories — staged 0 skills.`);
      } else {
        console.log(
          `[bundle-skills] staged ${skills.length} skill(s): ${skills.map((s) => s.name).join(', ')}`,
        );
      }
    }
  } catch (err) {
    console.error(`[bundle-skills] ${err instanceof Error ? err.message : err}`);
    process.exit(1);
    return;
  }

  try {
    const source = await resolveSource(PLAYWRIGHT_SOURCE_ENV, {
      requireIt: false,
      requireFlag: '--require-skills',
    });
    if (source.skip) {
      console.warn(
        `[bundle-skills] ${PLAYWRIGHT_SOURCE_ENV} is unset — building without a shared ` +
          'browser; browser-driven skills will report the browser as unavailable.',
      );
      await writeMissingMarker(
        PLAYWRIGHT_STAGING_DIR,
        PLAYWRIGHT_MISSING_MARKER,
        PLAYWRIGHT_SOURCE_ENV,
        'Playwright browsers',
      );
    } else {
      const browsers = await stagePlaywrightBrowsers(source.dir);
      if (browsers.length === 0) {
        console.warn(
          `[bundle-skills] ${source.dir} holds no browser directories — staged 0 browsers.`,
        );
      } else {
        console.log(
          `[bundle-skills] staged shared browser(s): ${browsers.map((b) => b.name).join(', ')}`,
        );
      }
    }
  } catch (err) {
    console.error(`[bundle-skills] ${err instanceof Error ? err.message : err}`);
    process.exit(1);
    return;
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  await main();
}
