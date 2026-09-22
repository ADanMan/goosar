#!/usr/bin/env node

import { copyFile, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { access } from 'node:fs/promises';
import { constants } from 'node:fs';
import { homedir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const desktopRoot = resolve(here, '..');

export const STAGING_DIR = join(desktopRoot, 'resources-ca');

const MISSING_MARKER = 'CA-NOT-BUNDLED.txt';
export const CA_BUNDLE = 'ca-bundle.pem';
export const CORP_CA = 'corp-ca.pem';

async function exists(path) {
  try {
    await access(path, constants.F_OK);
    return true;
  } catch {
    return false;
  }
}

export function countCertificates(pemText) {
  return (pemText.match(/-----BEGIN CERTIFICATE-----/g) ?? []).length;
}

async function resolveSourceDir() {
  const explicit = process.env['GOOSAR_CA_BUNDLE_DIR']?.trim();
  if (explicit) return { dir: resolve(explicit), source: 'GOOSAR_CA_BUNDLE_DIR' };
  return { dir: join(homedir(), '.hermes'), source: '~/.hermes' };
}

async function writeMissingMarker(reason) {
  await rm(STAGING_DIR, { recursive: true, force: true });
  await mkdir(STAGING_DIR, { recursive: true });
  await writeFile(
    join(STAGING_DIR, MISSING_MARKER),
    'No corporate CA bundle was included in this build.\n\n' +
      `Reason: ${reason}\n\n` +
      'Perimeter («Контур») connections need ~/.hermes/ca-bundle.pem; a\n' +
      'build without it works outside the perimeter only, and the desktop\n' +
      'settings say so. See apps/desktop/scripts/bundle-ca.mjs for the\n' +
      'recipe and source resolution.\n',
    'utf-8',
  );
}

async function main() {
  const requireCa = process.argv.slice(2).includes('--require-ca');

  const fail = async (reason) => {
    if (requireCa) {
      console.error(`[bundle-ca] ${reason}`);
      process.exit(1);
    }
    console.warn(`[bundle-ca] ${reason} — building without CA bundles.`);
    await writeMissingMarker(reason);
    process.exit(0);
  };

  const { dir, source } = await resolveSourceDir();
  const bundlePath = join(dir, CA_BUNDLE);
  if (!(await exists(bundlePath))) {
    await fail(`no ${CA_BUNDLE} found in ${dir} (source: ${source})`);
    return;
  }

  const bundleText = await readFile(bundlePath, 'utf-8');
  const bundleCerts = countCertificates(bundleText);
  if (bundleCerts === 0) {
    await fail(`${bundlePath} contains no PEM certificates`);
    return;
  }

  await rm(STAGING_DIR, { recursive: true, force: true });
  await mkdir(STAGING_DIR, { recursive: true });
  await copyFile(bundlePath, join(STAGING_DIR, CA_BUNDLE));
  console.log(`[bundle-ca] staged ${CA_BUNDLE} (${bundleCerts} certificates) from ${dir}`);

  const corpPath = join(dir, CORP_CA);
  if (await exists(corpPath)) {
    const corpCerts = countCertificates(await readFile(corpPath, 'utf-8'));
    if (corpCerts === 0) {
      console.warn(`[bundle-ca] ${corpPath} contains no PEM certificates — skipped`);
    } else {
      await copyFile(corpPath, join(STAGING_DIR, CORP_CA));
      console.log(`[bundle-ca] staged ${CORP_CA} (${corpCerts} certificates) from ${dir}`);
    }
  } else {
    console.warn(`[bundle-ca] no ${CORP_CA} in ${dir} — bundling ${CA_BUNDLE} only`);
  }
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) {
  main().catch((err) => {
    console.error('[bundle-ca] failed:', err);
    process.exit(1);
  });
}
