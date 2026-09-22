#!/usr/bin/env node

import { createRequire } from 'node:module';
import { execFileSync } from 'node:child_process';
import { readFileSync, unlinkSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';

if (process.platform !== 'darwin') process.exit(0);

const DESIRED_NAME = process.env.DESKTOP_APP_SUFFIX
  ? `Goosar Canary ${process.env.DESKTOP_APP_SUFFIX}`
  : 'Goosar Canary';

const require = createRequire(import.meta.url);
const electronBin = require('electron');
const plistPath = resolve(electronBin, '../../Info.plist');

function plistGet(key) {
  try {
    return execFileSync('/usr/libexec/PlistBuddy', ['-c', `Print :${key}`, plistPath], {
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
    }).trim();
  } catch {
    return '';
  }
}

function plistSet(key, value) {
  try {
    execFileSync('/usr/libexec/PlistBuddy', ['-c', `Set :${key} ${value}`, plistPath]);
  } catch {
    execFileSync('/usr/libexec/PlistBuddy', ['-c', `Add :${key} string ${value}`, plistPath]);
  }
}

if (plistGet('CFBundleName') === DESIRED_NAME && plistGet('CFBundleDisplayName') === DESIRED_NAME) {
  process.exit(0);
}

const original = readFileSync(plistPath);
unlinkSync(plistPath);
writeFileSync(plistPath, original);

plistSet('CFBundleName', DESIRED_NAME);
plistSet('CFBundleDisplayName', DESIRED_NAME);

console.log(`[brand-dev-electron] ${plistPath} → CFBundleName="${DESIRED_NAME}"`);
