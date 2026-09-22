#!/usr/bin/env node

import { access, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { constants } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const desktopRoot = resolve(here, '..');

export const STAGING_DIR = join(desktopRoot, 'resources-deployment');

export const DEFAULTS_FILENAME = 'deployment.json';
const MISSING_MARKER = 'DEPLOYMENT-DEFAULTS-NOT-BUNDLED.txt';

export const SOURCE_ENV = 'GOOSAR_DESKTOP_DEPLOYMENT_DEFAULTS';

async function exists(path) {
  try {
    await access(path, constants.F_OK);
    return true;
  } catch {
    return false;
  }
}

export function normalizeDeploymentDefaults(raw) {
  let parsed;
  try {
    parsed = JSON.parse(raw);
  } catch (err) {
    throw new Error(
      `deployment defaults are not valid JSON: ${err instanceof Error ? err.message : err}`,
    );
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('deployment defaults top level must be a JSON object');
  }
  return { parsed, json: `${JSON.stringify(parsed, null, 2)}\n` };
}

async function writeMissingMarker(reason) {
  await rm(STAGING_DIR, { recursive: true, force: true });
  await mkdir(STAGING_DIR, { recursive: true });
  await writeFile(
    join(STAGING_DIR, MISSING_MARKER),
    'No deployment defaults were baked into this build.\n\n' +
      `Reason: ${reason}\n\n` +
      `Point ${SOURCE_ENV} at a JSON file shaped like ~/.goosar/desktop.json\n` +
      "to bake a deployment's own server address and Kerberos realm into the\n" +
      'app bundle. Absent, the app falls back to DEFAULT_RUNTIME_CONFIG and the\n' +
      'user (or the operator, per machine) supplies ~/.goosar/desktop.json.\n',
    'utf-8',
  );
}

export async function stageDeploymentDefaults(sourceFile) {
  const raw = await readFile(sourceFile, 'utf-8');
  const { json } = normalizeDeploymentDefaults(raw);
  await rm(STAGING_DIR, { recursive: true, force: true });
  await mkdir(STAGING_DIR, { recursive: true });
  const target = join(STAGING_DIR, DEFAULTS_FILENAME);
  await writeFile(target, json, 'utf-8');
  return { target };
}

async function main() {
  const source = process.env[SOURCE_ENV]?.trim();

  if (!source) {
    console.warn(
      `[bundle-deployment-defaults] ${SOURCE_ENV} is unset — building without baked deployment defaults.`,
    );
    await writeMissingMarker(`${SOURCE_ENV} is unset`);
    process.exit(0);
  }

  const resolved = resolve(source);
  if (!(await exists(resolved))) {
    console.error(
      `[bundle-deployment-defaults] ${SOURCE_ENV}=${resolved} does not exist. An ` +
        'explicit path that is not present is a build mistake; unset ' +
        `${SOURCE_ENV} to build without deployment defaults on purpose.`,
    );
    process.exit(1);
  }

  try {
    const { target } = await stageDeploymentDefaults(resolved);
    console.log(`[bundle-deployment-defaults] staged defaults → ${target}`);
  } catch (err) {
    console.error(
      `[bundle-deployment-defaults] ${SOURCE_ENV}=${resolved}: ` +
        `${err instanceof Error ? err.message : err}`,
    );
    process.exit(1);
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  await main();
}
