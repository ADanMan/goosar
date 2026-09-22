import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { execFileSync } from 'node:child_process';
import {
  appendFileSync,
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
import { join } from 'node:path';

import { hashArtifactTree } from '../../scripts/hash-artifact-tree.mjs';
import type { AgentPathContext } from './agent-bootstrap';
import { ensureBundledSkills, playwrightBrowsersEnv, readSkillsStatus, skillsRoot } from './skills';

let home: string;
let bundled: string;

function ctx(overrides: Partial<AgentPathContext> = {}): AgentPathContext {
  return { home, env: { HOME: home }, ...overrides };
}

async function makeBundledSkill(name: string, marker: string): Promise<void> {
  const dir = join(bundled, name);
  mkdirSync(join(dir, 'scripts'), { recursive: true });
  writeFileSync(join(dir, 'SKILL.md'), `# ${name}\n${marker}\n`);
  writeFileSync(join(dir, 'scripts', 'run.py'), marker);
  execFileSync('ln', ['-s', '../../shared/lib', join(dir, 'scripts', 'lib')]);
  const sha256 = await hashArtifactTree(dir);
  appendFileSync(join(bundled, 'SKILLS-MANIFEST.txt'), `entry=${name} sha256=${sha256}\n`);
}

beforeEach(() => {
  home = mkdtempSync(join(tmpdir(), 'goosar-skills-home-'));
  bundled = mkdtempSync(join(tmpdir(), 'goosar-skills-bundled-'));
});

afterEach(() => {
  rmSync(home, { recursive: true, force: true });
  rmSync(bundled, { recursive: true, force: true });
});

describe('readSkillsStatus', () => {
  it('reports no skills on a fresh machine', async () => {
    await expect(readSkillsStatus(ctx())).resolves.toEqual({ installed: [] });
  });
});

describe('ensureBundledSkills', () => {
  it('does nothing when this build carries no bundled skills', async () => {
    const status = await ensureBundledSkills(ctx(), null);
    expect(status).toEqual({
      installed: [],
      staged: [],
      skippedExisting: [],
      refused: [],
    });
  });

  it('stages an absent skill, preserving its symlinks', async () => {
    await makeBundledSkill('slides', 'v1');

    const status = await ensureBundledSkills(ctx(), bundled);

    expect(status.staged).toEqual(['slides']);
    expect(status.skippedExisting).toEqual([]);
    const target = join(skillsRoot(ctx()), 'slides');
    expect(readFileSync(join(target, 'SKILL.md'), 'utf-8')).toContain('v1');
    const link = join(target, 'scripts', 'lib');
    expect(lstatSync(link).isSymbolicLink()).toBe(true);
    expect(readlinkSync(link)).toBe('../../shared/lib');
  });

  it('never overwrites a skill the user already has', async () => {
    const target = join(skillsRoot(ctx()), 'slides');
    mkdirSync(target, { recursive: true });
    writeFileSync(join(target, 'SKILL.md'), 'user-edited');

    await makeBundledSkill('slides', 'shipped');
    const status = await ensureBundledSkills(ctx(), bundled);

    expect(status.staged).toEqual([]);
    expect(status.skippedExisting).toEqual(['slides']);
    expect(readFileSync(join(target, 'SKILL.md'), 'utf-8')).toBe('user-edited');
  });

  it('stages only the absent skills when some already exist', async () => {
    const existing = join(skillsRoot(ctx()), 'slides');
    mkdirSync(existing, { recursive: true });
    writeFileSync(join(existing, 'SKILL.md'), 'user-edited');

    await makeBundledSkill('slides', 'shipped');
    await makeBundledSkill('docx', 'shipped');

    const status = await ensureBundledSkills(ctx(), bundled);

    expect(status.staged).toEqual(['docx']);
    expect(status.skippedExisting).toEqual(['slides']);
    expect(status.installed.sort()).toEqual(['docx', 'slides']);
  });

  it('is idempotent — a second run stages nothing new', async () => {
    await makeBundledSkill('slides', 'v1');
    await ensureBundledSkills(ctx(), bundled);
    const second = await ensureBundledSkills(ctx(), bundled);
    expect(second.staged).toEqual([]);
    expect(second.skippedExisting).toEqual(['slides']);
  });

  it('refuses a skill whose tree no longer matches the manifest, but stages the good ones', async () => {
    await makeBundledSkill('slides', 'v1');
    await makeBundledSkill('docx', 'v1');
    writeFileSync(join(bundled, 'slides', 'SKILL.md'), '# tampered\n');

    const status = await ensureBundledSkills(ctx(), bundled);

    expect(status.refused).toEqual(['slides']);
    expect(status.staged).toEqual(['docx']);
    expect(status.installed).toEqual(['docx']);
    expect(existsSync(join(skillsRoot(ctx()), 'slides'))).toBe(false);
  });

  it('refuses a skill that the manifest does not cover', async () => {
    await makeBundledSkill('slides', 'v1'); 
    mkdirSync(join(bundled, 'rogue'), { recursive: true });
    writeFileSync(join(bundled, 'rogue', 'SKILL.md'), '# rogue\n');

    const status = await ensureBundledSkills(ctx(), bundled);

    expect(status.staged).toEqual(['slides']);
    expect(status.refused).toEqual(['rogue']);
    expect(existsSync(join(skillsRoot(ctx()), 'rogue'))).toBe(false);
  });
});

describe('playwrightBrowsersEnv', () => {
  it('returns nothing when this build carries no bundled browser and nothing is provisioned', () => {
    expect(playwrightBrowsersEnv(null, null, {})).toEqual({});
  });

  it('points PLAYWRIGHT_BROWSERS_PATH at the bundled browser when present and it carries real content', () => {
    expect(
      playwrightBrowsersEnv({ dir: '/app/Resources/playwright', hasBrowsers: true }, null, {}),
    ).toEqual({
      PLAYWRIGHT_BROWSERS_PATH: '/app/Resources/playwright',
    });
  });

  it('leaves an explicit operator PLAYWRIGHT_BROWSERS_PATH untouched', () => {
    expect(
      playwrightBrowsersEnv({ dir: '/app/Resources/playwright', hasBrowsers: true }, null, {
        PLAYWRIGHT_BROWSERS_PATH: '/opt/my-browsers',
      }),
    ).toEqual({});
  });

  it('ignores a marker-only bundled dir (hasBrowsers: false) instead of pointing at it', () => {
    expect(
      playwrightBrowsersEnv({ dir: '/app/Resources/playwright', hasBrowsers: false }, null, {}),
    ).toEqual({});
  });

  it('prefers a provisioned runtime over the bundled browser', () => {
    expect(
      playwrightBrowsersEnv(
        { dir: '/app/Resources/playwright', hasBrowsers: true },
        '/Users/me/.hermes/runtime/playwright-browsers/playwright-browsers@2.0.0',
        {},
      ),
    ).toEqual({
      PLAYWRIGHT_BROWSERS_PATH:
        '/Users/me/.hermes/runtime/playwright-browsers/playwright-browsers@2.0.0',
    });
  });

  it('falls back to the bundled browser when nothing is provisioned yet', () => {
    expect(
      playwrightBrowsersEnv({ dir: '/app/Resources/playwright', hasBrowsers: true }, null, {}),
    ).toEqual({ PLAYWRIGHT_BROWSERS_PATH: '/app/Resources/playwright' });
  });

  it('still lets an explicit operator value win over a provisioned runtime', () => {
    expect(
      playwrightBrowsersEnv(
        { dir: '/app/Resources/playwright', hasBrowsers: true },
        '/Users/me/.hermes/runtime/playwright-browsers/playwright-browsers@2.0.0',
        { PLAYWRIGHT_BROWSERS_PATH: '/opt/my-browsers' },
      ),
    ).toEqual({});
  });
});
