#!/usr/bin/env node

import { access, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { constants } from 'node:fs';
import { createHash } from 'node:crypto';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const desktopRoot = resolve(here, '..');

export const STAGING_DIR = join(desktopRoot, 'resources-preset-overlay');

export const OVERLAY_FILENAME = 'preset-overlay.json';
const MISSING_MARKER = 'PRESET-OVERLAY-NOT-BUNDLED.txt';
export const MANIFEST = 'PRESET-OVERLAY-MANIFEST.txt';

export const SOURCE_ENV = 'GOOSAR_PRESET_OVERLAY';

async function exists(path) {
  try {
    await access(path, constants.F_OK);
    return true;
  } catch {
    return false;
  }
}

export function normalizeOverlay(raw) {
  let parsed;
  try {
    parsed = JSON.parse(raw);
  } catch (err) {
    throw new Error(`overlay is not valid JSON: ${err instanceof Error ? err.message : err}`);
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('overlay top level must be a JSON object');
  }
  return { parsed, json: `${JSON.stringify(parsed, null, 2)}\n` };
}

export function findBakedCredentials(parsed) {
  const found = [];
  const secretName = /token|secret|password|api[_-]?key|credential/i;
  const bearer = /bearer\s+\S/i;

  const servers = parsed?.mcpServers;
  if (!servers || typeof servers !== 'object') return found;
  for (const [name, entry] of Object.entries(servers)) {
    if (!entry || typeof entry !== 'object') continue;
    for (const arg of Array.isArray(entry.args) ? entry.args : []) {
      if (typeof arg === 'string' && bearer.test(arg)) {
        found.push(`${name}.args carries a Bearer credential`);
      }
    }
    const env = entry.env && typeof entry.env === 'object' ? entry.env : {};
    for (const [key, value] of Object.entries(env)) {
      if (secretName.test(key) && typeof value === 'string' && value.trim() !== '') {
        found.push(`${name}.env.${key} carries a value`);
      }
    }
  }
  return found;
}

async function writeMissingMarker(reason) {
  await rm(STAGING_DIR, { recursive: true, force: true });
  await mkdir(STAGING_DIR, { recursive: true });
  await writeFile(
    join(STAGING_DIR, MISSING_MARKER),
    'No MCP preset overlay was bundled into this build.\n\n' +
      `Reason: ${reason}\n\n` +
      `Point ${SOURCE_ENV} at a JSON overlay file to stage it. The overlay\n` +
      'pre-fills the placeholder MCP preset addresses at first run; corporate\n' +
      'overlays are injected this way at build time from the internal\n' +
      'provisioning bundle and are never committed to this repository.\n' +
      'Absent, the placeholder presets stand and the user fills them in at\n' +
      'onboarding.\n',
    'utf-8',
  );
}

async function writeManifest({ source, json }) {
  const sha256 = createHash('sha256').update(json, 'utf-8').digest('hex');
  const lines = [
    'MCP preset overlay staged into this build.',
    '',
    `source=${source}`,
    `bytes=${Buffer.byteLength(json, 'utf-8')}`,
    `sha256=${sha256}`,
    `staged_at=${new Date().toISOString()}`,
    '',
  ];
  await writeFile(join(STAGING_DIR, MANIFEST), lines.join('\n'), 'utf-8');
  return sha256;
}

export async function stageOverlay(sourceFile, { allowSecrets = false } = {}) {
  const raw = await readFile(sourceFile, 'utf-8');
  const { parsed, json } = normalizeOverlay(raw);
  const baked = findBakedCredentials(parsed);
  if (baked.length > 0) {
    const summary = baked.join('; ');
    if (!allowSecrets) {
      throw new Error(
        `overlay bakes a credential into every installer (${summary}). ` +
          'It would ship in cleartext to every recipient and could only be ' +
          'rotated by rebuilding and redistributing. Put it in the per-user ' +
          'MCP configuration instead, or pass --allow-preset-overlay-secrets ' +
          'if this is deliberate.',
      );
    }
    console.warn(
      `[bundle-preset-overlay] baking a credential into every installer (${summary}) — --allow-preset-overlay-secrets was passed.`,
    );
  }
  await rm(STAGING_DIR, { recursive: true, force: true });
  await mkdir(STAGING_DIR, { recursive: true });
  const target = join(STAGING_DIR, OVERLAY_FILENAME);
  await writeFile(target, json, 'utf-8');
  const sha256 = await writeManifest({ source: sourceFile, json });
  return { target, sha256 };
}

async function main() {
  const argv = process.argv.slice(2);
  const requireOverlay = argv.includes('--require-preset-overlay');
  const allowSecrets =
    argv.includes('--allow-preset-overlay-secrets') ||
    process.env.GOOSAR_PRESET_OVERLAY_ALLOW_SECRETS === '1';
  const source = process.env[SOURCE_ENV]?.trim();

  if (!source) {
    if (requireOverlay) {
      console.error(
        `[bundle-preset-overlay] ${SOURCE_ENV} is unset but ` +
          '--require-preset-overlay was passed — refusing to build without the ' +
          'MCP preset overlay this release is expected to carry.',
      );
      process.exit(1);
    }
    console.warn(
      `[bundle-preset-overlay] ${SOURCE_ENV} is unset — building without a preset overlay.`,
    );
    await writeMissingMarker(`${SOURCE_ENV} is unset`);
    process.exit(0);
  }

  const resolved = resolve(source);
  if (!(await exists(resolved))) {
    console.error(
      `[bundle-preset-overlay] ${SOURCE_ENV}=${resolved} does not exist. An ` +
        'explicit overlay path that is not present is a build mistake; unset ' +
        `${SOURCE_ENV} to build without an overlay on purpose.`,
    );
    process.exit(1);
  }

  try {
    const { target, sha256 } = await stageOverlay(resolved, { allowSecrets });
    console.log(`[bundle-preset-overlay] staged overlay → ${target} (sha256=${sha256})`);
  } catch (err) {
    console.error(
      `[bundle-preset-overlay] ${SOURCE_ENV}=${resolved}: ` +
        `${err instanceof Error ? err.message : err}`,
    );
    process.exit(1);
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  await main();
}
