import { execFile } from 'child_process';
import { existsSync, promises as fs } from 'fs';
import { join } from 'path';

import { agentHomeDir, type AgentPathContext } from './agent-bootstrap';
import { readBundleManifest, verifyBundledEntry } from './bundle-manifest';

const SKILLS_MANIFEST = 'SKILLS-MANIFEST.txt';

export function skillsRoot(ctx: AgentPathContext): string {
  return join(agentHomeDir(ctx), 'skills');
}

export interface SkillsStatus {
  installed: string[];
  staged: string[];
  skippedExisting: string[];
  refused: string[];
}

async function skillDirNames(dir: string): Promise<string[]> {
  try {
    const entries = await fs.readdir(dir, { withFileTypes: true });
    return entries
      .filter((entry) => entry.isDirectory() && !entry.name.startsWith('.'))
      .map((entry) => entry.name)
      .sort();
  } catch {
    return [];
  }
}

function copyTree(src: string, dest: string): Promise<void> {
  const flags = process.platform === 'darwin' ? ['-Rc'] : ['-R'];
  return new Promise((resolve, reject) => {
    execFile('cp', [...flags, src, dest], (err) => {
      if (err) reject(err);
      else resolve();
    });
  });
}

export async function readSkillsStatus(
  ctx: AgentPathContext,
): Promise<Pick<SkillsStatus, 'installed'>> {
  return { installed: await skillDirNames(skillsRoot(ctx)) };
}

async function stageOneSkill(
  bundledDir: string,
  name: string,
  root: string,
): Promise<'staged' | 'skipped' | 'failed'> {
  const target = join(root, name);
  if (existsSync(target)) return 'skipped';

  const temp = join(root, `.${name}.staging-${process.pid}-${Date.now()}`);
  try {
    await fs.rm(temp, { recursive: true, force: true });
    await copyTree(join(bundledDir, name), temp);
    try {
      await fs.rename(temp, target);
    } catch {
      if (existsSync(target)) {
        await fs.rm(temp, { recursive: true, force: true });
        return 'skipped';
      }
      throw new Error(`could not move staged skill into ${target}`);
    }
    console.log(`[skills] staged ${name} → ${target}`);
    return 'staged';
  } catch (err) {
    await fs.rm(temp, { recursive: true, force: true }).catch(() => {});
    console.warn(`[skills] could not stage ${name}:`, err);
    return 'failed';
  }
}

export async function ensureBundledSkills(
  ctx: AgentPathContext,
  bundledDir: string | null,
): Promise<SkillsStatus> {
  const root = skillsRoot(ctx);
  const staged: string[] = [];
  const skippedExisting: string[] = [];
  const refused: string[] = [];

  if (bundledDir) {
    const names = await skillDirNames(bundledDir);
    if (names.length > 0) {
      const manifest = await readBundleManifest(bundledDir, SKILLS_MANIFEST, 'entry');
      try {
        await fs.mkdir(root, { recursive: true });
        for (const name of names) {
          const check = await verifyBundledEntry(bundledDir, name, manifest);
          if (!check.ok) {
            console.error(`[skills] refusing to stage ${name}: ${check.reason}`);
            refused.push(name);
            continue;
          }
          const outcome = await stageOneSkill(bundledDir, name, root);
          if (outcome === 'staged') staged.push(name);
          else if (outcome === 'skipped') skippedExisting.push(name);
        }
      } catch (err) {
        console.warn('[skills] skill materialization failed:', err);
      }
    }
  }

  const { installed } = await readSkillsStatus(ctx);
  return { installed, staged, skippedExisting, refused };
}

export interface PlaywrightBundleInfo {
  dir: string;
  hasBrowsers: boolean;
}

export function playwrightBrowsersEnv(
  bundled: PlaywrightBundleInfo | null,
  provisionedBrowsersDir: string | null,
  currentEnv: NodeJS.ProcessEnv,
): Record<string, string> {
  if (currentEnv.PLAYWRIGHT_BROWSERS_PATH?.trim()) return {};
  if (provisionedBrowsersDir) {
    return { PLAYWRIGHT_BROWSERS_PATH: provisionedBrowsersDir };
  }
  if (bundled?.hasBrowsers) {
    return { PLAYWRIGHT_BROWSERS_PATH: bundled.dir };
  }
  return {};
}
