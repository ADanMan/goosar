import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { execFileSync, spawnSync } from 'node:child_process';
import {
  existsSync,
  lstatSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readlinkSync,
  rmSync,
  writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { stagePlaywrightBrowsers, stageSkills } from './bundle-skills.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const script = join(here, 'bundle-skills.mjs');
const desktopRoot = resolve(here, '..');
const skillsStagingDir = join(desktopRoot, 'resources-skills');
const playwrightStagingDir = join(desktopRoot, 'resources-playwright');

let work;

function makeSkill(name, { withSkillMd = true } = {}) {
  const dir = join(work, 'skills', name);
  mkdirSync(join(dir, 'scripts'), { recursive: true });
  if (withSkillMd) writeFileSync(join(dir, 'SKILL.md'), `# ${name}\n`);
  writeFileSync(join(dir, 'scripts', 'run.py'), "print('hi')\n");
  execFileSync('ln', ['-s', '../../shared/lib', join(dir, 'scripts', 'lib')]);
  return dir;
}

function makeBrowsers() {
  const dir = join(work, 'browsers');
  const chromium = join(dir, 'chromium-1200', 'chrome-linux');
  mkdirSync(chromium, { recursive: true });
  writeFileSync(join(chromium, 'chrome'), '#!/bin/sh\nexit 0\n');
  return dir;
}

function readManifest(stagingDir, name) {
  const text = readFileSync(join(stagingDir, name), 'utf-8');
  const entries = {};
  let count = null;
  for (const line of text.split('\n')) {
    const countMatch = line.match(/^count=(\d+)$/);
    if (countMatch) count = Number.parseInt(countMatch[1], 10);
    const entryMatch = line.match(/^entry=(\S+) sha256=([0-9a-f]{64})$/);
    if (entryMatch) entries[entryMatch[1]] = { sha256: entryMatch[2] };
  }
  return { count, entries };
}

function runScript(env = {}, args = []) {
  const result = spawnSync('node', [script, ...args], {
    cwd: desktopRoot,
    encoding: 'utf-8',
    env: {
      ...process.env,
      GOOSAR_SKILLS_DIR: '',
      GOOSAR_PLAYWRIGHT_BROWSERS_DIR: '',
      ...env,
    },
  });
  return {
    status: result.status,
    output: `${result.stdout ?? ''}${result.stderr ?? ''}`,
  };
}

beforeEach(() => {
  work = mkdtempSync(join(tmpdir(), 'bundle-skills-'));
  mkdirSync(join(work, 'skills'), { recursive: true });
  rmSync(skillsStagingDir, { recursive: true, force: true });
  rmSync(playwrightStagingDir, { recursive: true, force: true });
});

afterEach(() => {
  rmSync(work, { recursive: true, force: true });
  rmSync(skillsStagingDir, { recursive: true, force: true });
  rmSync(playwrightStagingDir, { recursive: true, force: true });
});

describe('stageSkills', () => {
  it('stages a skill tree, preserving symlinks, and returns manifest rows', async () => {
    makeSkill('slides');
    const rows = await stageSkills(join(work, 'skills'));
    expect(rows.map((r) => r.name)).toEqual(['slides']);
    expect(rows[0].sha256).toMatch(/^[0-9a-f]{64}$/);
    const link = join(skillsStagingDir, 'slides', 'scripts', 'lib');
    expect(lstatSync(link).isSymbolicLink()).toBe(true);
    expect(readlinkSync(link)).toBe('../../shared/lib');
  });

  it("ships the skill's LICENSE and NOTICE next to the skill", async () => {
    const dir = makeSkill('slides');
    writeFileSync(join(dir, 'LICENSE'), 'Apache License 2.0\n');
    writeFileSync(join(dir, 'NOTICE'), 'notice\n');
    await stageSkills(join(work, 'skills'));
    const staged = join(skillsStagingDir, 'slides');
    expect(readFileSync(join(staged, 'LICENSE'), 'utf-8')).toBe('Apache License 2.0\n');
    expect(readFileSync(join(staged, 'NOTICE'), 'utf-8')).toBe('notice\n');
  });
});

describe('stagePlaywrightBrowsers', () => {
  it('stages the browsers root and returns manifest rows', async () => {
    const browsers = makeBrowsers();
    const rows = await stagePlaywrightBrowsers(browsers);
    expect(rows.map((r) => r.name)).toEqual(['chromium-1200']);
    expect(existsSync(join(playwrightStagingDir, 'chromium-1200', 'chrome-linux', 'chrome'))).toBe(
      true,
    );
  });

  it('stages root-level identity files but not the local install marker', async () => {
    const browsers = makeBrowsers();
    writeFileSync(
      join(browsers, 'provisioning-package.json'),
      JSON.stringify({ name: 'playwright-browsers', version: '1.62.0', type: 'runtime' }),
    );
    writeFileSync(join(browsers, '.provisioned.json'), '{}\n');

    const rows = await stagePlaywrightBrowsers(browsers);

    expect(existsSync(join(playwrightStagingDir, 'provisioning-package.json'))).toBe(true);
    expect(existsSync(join(playwrightStagingDir, '.provisioned.json'))).toBe(false);
    expect(rows.map((r) => r.name).sort()).toEqual(['chromium-1200', 'provisioning-package.json']);
    for (const row of rows) expect(row.sha256).toMatch(/^[0-9a-f]{64}$/);
  });
});

describe('bundle-skills CLI — skills', () => {
  it('stages nothing and writes a marker when the skills env is unset', () => {
    const { status, output } = runScript();
    expect(status).toBe(0);
    expect(output).toMatch(/building without skills/);
    expect(existsSync(join(skillsStagingDir, 'SKILLS-NOT-BUNDLED.txt'))).toBe(true);
  });

  it('hard-fails when the skills env points at a non-existent directory', () => {
    const { status, output } = runScript({
      GOOSAR_SKILLS_DIR: join(work, 'does-not-exist'),
    });
    expect(status).toBe(1);
    expect(output).toMatch(/does not exist/);
  });

  it('hard-fails when unset but --require-skills is passed', () => {
    const { status, output } = runScript({}, ['--require-skills']);
    expect(status).toBe(1);
    expect(output).toMatch(/refusing to build without content/);
  });

  it('stages skills, preserving symlinks, and writes a manifest', () => {
    makeSkill('slides');
    makeSkill('docx');

    const { status, output } = runScript({ GOOSAR_SKILLS_DIR: join(work, 'skills') });

    expect(status).toBe(0);
    expect(output).toMatch(/staged 2 skill\(s\): docx, slides/);
    const link = join(skillsStagingDir, 'slides', 'scripts', 'lib');
    expect(lstatSync(link).isSymbolicLink()).toBe(true);
    expect(readlinkSync(link)).toBe('../../shared/lib');
    const manifest = readManifest(skillsStagingDir, 'SKILLS-MANIFEST.txt');
    expect(manifest.count).toBe(2);
    expect(Object.keys(manifest.entries).sort()).toEqual(['docx', 'slides']);
    expect(manifest.entries.slides.sha256).toMatch(/^[0-9a-f]{64}$/);
  });

  it('stages zero skills for an existing-but-empty source dir', () => {
    const { status } = runScript({ GOOSAR_SKILLS_DIR: join(work, 'skills') });
    expect(status).toBe(0);
    const manifest = readManifest(skillsStagingDir, 'SKILLS-MANIFEST.txt');
    expect(manifest.count).toBe(0);
  });

  it('stages a skill that is a symlink to a directory elsewhere (issue #696)', () => {
    const real = makeSkill('mail-triage');
    const target = join(work, 'elsewhere', 'mail-triage');
    mkdirSync(dirname(target), { recursive: true });
    execFileSync('mv', [real, target]);
    execFileSync('ln', ['-s', target, join(work, 'skills', 'mail-triage')]);

    const { status, output } = runScript({ GOOSAR_SKILLS_DIR: join(work, 'skills') });

    expect(status).toBe(0);
    expect(output).toMatch(/staged 1 skill\(s\): mail-triage/);
    expect(existsSync(join(skillsStagingDir, 'mail-triage', 'SKILL.md'))).toBe(true);
  });

  it('skips a plain file and a broken symlink with a WARN, and keeps building', () => {
    makeSkill('slides');
    writeFileSync(join(work, 'skills', 'README.txt'), 'not a skill\n');
    execFileSync('ln', ['-s', join(work, 'skills', 'nope'), join(work, 'skills', 'ghost')]);

    const { status, output } = runScript({ GOOSAR_SKILLS_DIR: join(work, 'skills') });

    expect(status).toBe(0);
    expect(output).toMatch(/WARN: skipped README\.txt: not a directory/);
    expect(output).toMatch(/WARN: skipped ghost: broken symlink/);
    expect(output).toMatch(/staged 1 skill\(s\): slides/);
  });

  it('FAILS with --require-skills when a source entry was skipped', () => {
    makeSkill('slides');
    writeFileSync(join(work, 'skills', 'README.txt'), 'not a skill\n');

    const { status, output } = runScript({ GOOSAR_SKILLS_DIR: join(work, 'skills') }, [
      '--require-skills',
    ]);

    expect(status).toBe(1);
    expect(output).toMatch(/README\.txt/);
  });
});

describe('bundle-skills CLI — shared Playwright/Chromium', () => {
  it('writes a marker when the browsers env is unset', () => {
    const { status, output } = runScript();
    expect(status).toBe(0);
    expect(output).toMatch(/building without a shared browser/);
    expect(existsSync(join(playwrightStagingDir, 'PLAYWRIGHT-NOT-BUNDLED.txt'))).toBe(true);
  });

  it('hard-fails when the browsers env points at a non-existent directory', () => {
    const { status, output } = runScript({
      GOOSAR_PLAYWRIGHT_BROWSERS_DIR: join(work, 'no-browsers'),
    });
    expect(status).toBe(1);
    expect(output).toMatch(/does not exist/);
  });

  it('stages the shared browser and writes a manifest', () => {
    const browsers = makeBrowsers();
    const { status, output } = runScript({ GOOSAR_PLAYWRIGHT_BROWSERS_DIR: browsers });
    expect(status).toBe(0);
    expect(output).toMatch(/staged shared browser\(s\): chromium-1200/);
    const manifest = readManifest(playwrightStagingDir, 'PLAYWRIGHT-MANIFEST.txt');
    expect(manifest.count).toBe(1);
    expect(Object.keys(manifest.entries)).toEqual(['chromium-1200']);
  });

  it('stages skills and a shared browser together in one run', () => {
    makeSkill('slides');
    const browsers = makeBrowsers();
    const { status } = runScript({
      GOOSAR_SKILLS_DIR: join(work, 'skills'),
      GOOSAR_PLAYWRIGHT_BROWSERS_DIR: browsers,
    });
    expect(status).toBe(0);
    expect(existsSync(join(skillsStagingDir, 'slides', 'SKILL.md'))).toBe(true);
    expect(existsSync(join(playwrightStagingDir, 'chromium-1200'))).toBe(true);
  });
});
