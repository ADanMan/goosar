#!/usr/bin/env node
import { execFileSync } from 'node:child_process';
import { mkdtempSync, rmSync, symlinkSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { basename, join } from 'node:path';

import {
  attachDmgReadOnly,
  compareInventories,
  detachDmg,
  walkTree,
} from './verify-packaged-artifact.mjs';

export const IMAGE_HEADROOM_MB = 900;

export function computeImageSizeMb(appPath, headroomMb = IMAGE_HEADROOM_MB) {
  const out = execFileSync('du', ['-sm', appPath], { encoding: 'utf-8' });
  const measured = Number.parseInt(out.trim().split(/\s+/)[0], 10);
  if (!Number.isFinite(measured)) {
    throw new Error(`could not measure app size: ${appPath}`);
  }
  return measured + headroomMb;
}

function attachReadWrite(imagePath) {
  const out = execFileSync(
    'hdiutil',
    ['attach', '-nobrowse', '-noverify', '-mountrandom', '/tmp', imagePath],
    { encoding: 'utf-8' },
  );
  const mounts = out
    .split('\n')
    .map((line) => line.split('\t').pop()?.trim() ?? '')
    .filter((p) => p.startsWith('/'));
  const mountPoint = mounts.pop();
  if (!mountPoint) throw new Error(`could not determine mount point for ${imagePath}`);
  return mountPoint;
}

export function buildMacDmg({
  appPath,
  outDmg,
  volname,
  headroomMb = IMAGE_HEADROOM_MB,
  log = console.log,
}) {
  const appName = basename(appPath);
  const sizeMb = computeImageSizeMb(appPath, headroomMb);
  const workDir = mkdtempSync(join(tmpdir(), 'goosar-dmg-'));
  const stageImage = join(workDir, 'stage.dmg');
  log(`[dmg] ${appName} → ${basename(outDmg)} (staging image ${sizeMb}MB)`);
  try {
    execFileSync(
      'hdiutil',
      [
        'create',
        '-size',
        `${sizeMb}m`,
        '-fs',
        'HFS+',
        '-volname',
        volname,
        '-type',
        'UDIF',
        stageImage,
      ],
      { stdio: 'ignore' },
    );
    const mountPoint = attachReadWrite(stageImage);
    try {
      execFileSync('ditto', [appPath, join(mountPoint, appName)], { stdio: 'inherit' });
      symlinkSync('/Applications', join(mountPoint, 'Applications'));
      const problems = compareInventories(walkTree(appPath), walkTree(join(mountPoint, appName)));
      if (problems.length > 0) {
        const detail = problems.slice(0, 10).join('\n    ');
        throw new Error(
          `image copy is incomplete — refusing to build ${basename(outDmg)}:\n    ${detail}` +
            (problems.length > 10 ? `\n    … and ${problems.length - 10} more` : ''),
        );
      }
      log(`[dmg] in-image copy verified (${problems.length} problems)`);
    } finally {
      detachDmg(mountPoint);
    }
    rmSync(outDmg, { force: true });
    execFileSync(
      'hdiutil',
      ['convert', stageImage, '-format', 'UDZO', '-imagekey', 'zlib-level=1', '-o', outDmg],
      { stdio: 'ignore' },
    );
    log(`[dmg] wrote ${outDmg}`);
    return outDmg;
  } finally {
    rmSync(workDir, { recursive: true, force: true });
  }
}

function parseArgv(argv) {
  const parsed = { app: null, out: null, volname: null };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === '--app') parsed.app = argv[++i];
    else if (arg === '--out') parsed.out = argv[++i];
    else if (arg === '--volname') parsed.volname = argv[++i];
    else throw new Error(`unknown option: ${arg}`);
  }
  return parsed;
}

function main() {
  let parsed;
  try {
    parsed = parseArgv(process.argv.slice(2));
  } catch (err) {
    console.error(`[dmg] ${err.message}`);
    process.exit(1);
  }
  if (!parsed.app || !parsed.out) {
    console.error('[dmg] usage: --app <App.app> --out <path.dmg> [--volname "<Name>"]');
    process.exit(1);
  }
  try {
    buildMacDmg({
      appPath: parsed.app,
      outDmg: parsed.out,
      volname: parsed.volname ?? basename(parsed.app, '.app'),
    });
  } catch (err) {
    console.error(`[dmg] FAILED: ${err.message}`);
    process.exit(1);
  }
}

if (process.argv[1] && import.meta.url === new URL(`file://${process.argv[1]}`).href) {
  main();
}
