import { promises as fs } from 'fs';
import { basename, dirname, join, sep } from 'path';

import {
  agentHomeDir,
  classifyUserBin,
  runtimeRoot,
  userBinPath,
  type AgentPathContext,
  type UserBinKind,
} from './agent-bootstrap';
import type {
  DaemonStopReport,
  KeptItem,
  ManualStep,
  UninstallFailure,
  UninstallItem,
  UninstallItemKind,
  UninstallOptions,
  UninstallOutcome,
  UninstallPlan,
} from '../shared/uninstall-types';

export interface UninstallContext extends AgentPathContext {
  platform: NodeJS.Platform;
  userDataDir: string;
  executablePath: string;
  isPackaged: boolean;
  stopDaemon: () => Promise<DaemonStopReport>;
}

interface Candidate {
  id: string;
  path: string;
  label: string;
  kind: UninstallItemKind;
}

function goosarHomeDir(ctx: AgentPathContext): string {
  return join(ctx.home, '.goosar');
}

const DESKTOP_PROFILE_PREFIX = 'desktop-';

async function pathExists(path: string): Promise<boolean> {
  try {
    await fs.lstat(path);
    return true;
  } catch {
    return false;
  }
}

async function sizeOnDisk(path: string): Promise<number | null> {
  let stat: Awaited<ReturnType<typeof fs.lstat>>;
  try {
    stat = await fs.lstat(path);
  } catch {
    return null;
  }
  if (!stat.isDirectory()) return stat.size;

  let entries: string[];
  try {
    entries = await fs.readdir(path);
  } catch {
    return null;
  }
  let total = 0;
  for (const entry of entries) {
    const child = await sizeOnDisk(join(path, entry));
    if (child === null) return null;
    total += child;
  }
  return total;
}

async function materialize(candidates: readonly Candidate[]): Promise<UninstallItem[]> {
  const items: UninstallItem[] = [];
  for (const candidate of candidates) {
    if (!(await pathExists(candidate.path))) continue;
    items.push({ ...candidate, bytes: await sizeOnDisk(candidate.path) });
  }
  return items;
}

async function profileDirs(ctx: AgentPathContext): Promise<string[]> {
  const root = join(goosarHomeDir(ctx), 'profiles');
  try {
    const entries = await fs.readdir(root, { withFileTypes: true });
    return entries.filter((entry) => entry.isDirectory()).map((entry) => join(root, entry.name));
  } catch {
    return [];
  }
}

async function classifyUserBinForPlatform(ctx: UninstallContext): Promise<UserBinKind> {
  if (ctx.platform === 'win32') return 'absent';
  return classifyUserBin(ctx);
}

function managedBinDir(ctx: AgentPathContext): string {
  return join(runtimeRoot(ctx), 'bin');
}

export type RuntimeOwnership = 'none' | 'managed' | 'shared' | 'foreign';

export async function classifyRuntimeOwnership(ctx: UninstallContext): Promise<RuntimeOwnership> {
  if (!(await pathExists(runtimeRoot(ctx)))) return 'none';
  if (!(await pathExists(managedBinDir(ctx)))) return 'foreign';
  return (await classifyUserBinForPlatform(ctx)) === 'managed' ? 'shared' : 'managed';
}

export function installedBundlePath(
  ctx: Pick<UninstallContext, 'platform' | 'executablePath' | 'isPackaged'>,
): string | null {
  if (!ctx.isPackaged) return null;
  if (ctx.platform === 'darwin') {
    const marker = ctx.executablePath.indexOf(`.app${sep}`);
    return marker === -1 ? null : ctx.executablePath.slice(0, marker + 4);
  }
  return dirname(ctx.executablePath);
}

function manualSteps(ctx: UninstallContext): ManualStep[] {
  const bundle = installedBundlePath(ctx);
  if (!bundle) return [];
  return [
    {
      path: bundle,
      instruction:
        ctx.platform === 'darwin'
          ? 'macOS does not let an app delete itself while it is running. Quit Goosar, then move this to the Trash.'
          : 'The app cannot delete its own program files while it is running. Quit Goosar, then remove this folder.',
    },
  ];
}

function agentServiceCandidates(ctx: UninstallContext): Candidate[] {
  const agentHome = agentHomeDir(ctx);
  return [
    {
      id: 'agent-events',
      path: join(agentHome, '.event'),
      label: 'Agent event spool',
      kind: 'software',
    },
    {
      id: 'agent-logs',
      path: join(agentHome, '.logs'),
      label: 'Agent logs',
      kind: 'software',
    },
  ];
}

function softwareCandidates(
  ctx: UninstallContext,
  ownProfiles: readonly string[],
  runtimeIsOurs: boolean,
): Candidate[] {
  const goosar = goosarHomeDir(ctx);
  return [
    ...(runtimeIsOurs
      ? [
          {
            id: 'agent-runtime',
            path: runtimeRoot(ctx),
            label: 'Agent runtime — the bundled hermes interpreter and every version of it',
            kind: 'software' as const,
          },
          ...agentServiceCandidates(ctx),
        ]
      : []),
    {
      id: 'app-data',
      path: ctx.userDataDir,
      label: 'Desktop app data — window layout, caches, and the managed goosar CLI',
      kind: 'software',
    },
    {
      id: 'goosar-server-address',
      path: join(goosar, 'desktop.json'),
      label: 'The server address this app was pointed at',
      kind: 'software',
    },
    {
      id: 'goosar-daemon-prefs',
      path: join(goosar, 'desktop_prefs.json'),
      label: 'Daemon preferences (auto-start, auto-stop)',
      kind: 'software',
    },
    ...ownProfiles.map((path) => ({
      id: `goosar-profile:${basename(path)}`,
      path,
      label: `Daemon profile this app created (${basename(path)}), including its stored access token`,
      kind: 'software' as const,
    })),
  ];
}

function userDataCandidates(ctx: UninstallContext): Candidate[] {
  const agentHome = agentHomeDir(ctx);
  return [
    {
      id: 'agent-config',
      path: join(agentHome, 'config.user.yaml'),
      label: 'Agent configuration, including your LLM API key',
      kind: 'user_data',
    },
    {
      id: 'agent-agents',
      path: join(agentHome, 'agents'),
      label: 'Agents you wrote',
      kind: 'user_data',
    },
    {
      id: 'agent-skills',
      path: join(agentHome, 'skills'),
      label: 'Skills you wrote',
      kind: 'user_data',
    },
    {
      id: 'agent-crons',
      path: join(agentHome, 'crons'),
      label: 'Scheduled runs you set up',
      kind: 'user_data',
    },
    {
      id: 'agent-history',
      path: join(agentHome, '.history'),
      label: 'Your agent conversation history',
      kind: 'user_data',
    },
  ];
}

function runtimeKeptReason(
  ctx: UninstallContext,
  ownership: Exclude<RuntimeOwnership, 'none' | 'managed'>,
): string {
  if (ownership === 'shared') {
    return (
      `Both this app and a hermes you installed yourself have written here: ` +
      `${userBinPath(ctx)} links into this folder, which only hermes's own ` +
      `installer does. There is no way to tell which versions came from which, ` +
      `so none of them are removed. Delete this folder yourself if you want it ` +
      `gone.`
    );
  }
  return (
    `A hermes runtime that this app did not install — an install this app ` +
    `performs always leaves ${managedBinDir(ctx)} behind, and there is none. ` +
    `It is left exactly as it is.`
  );
}

function agentServiceKeptReason(
  ctx: UninstallContext,
  ownership: Exclude<RuntimeOwnership, 'managed'>,
): string {
  if (ownership === 'none') {
    return (
      'This app never installed a runtime here, so the service files in this ' +
      'folder were written by a hermes you installed yourself. They are left ' +
      'alone.'
    );
  }
  return (
    `Written by whichever hermes ran, and the runtime at ${runtimeRoot(ctx)} ` +
    `is not this app's to speak for. They are left alone.`
  );
}

async function keptItems(
  ctx: UninstallContext,
  userBin: UserBinKind,
  foreignProfiles: readonly string[],
  ownership: RuntimeOwnership,
): Promise<KeptItem[]> {
  const kept: KeptItem[] = [];
  if (ownership !== 'managed') {
    if (ownership !== 'none') {
      kept.push({
        path: runtimeRoot(ctx),
        reason: runtimeKeptReason(ctx, ownership),
      });
    }
    for (const stray of await materialize(agentServiceCandidates(ctx))) {
      kept.push({
        path: stray.path,
        reason: agentServiceKeptReason(ctx, ownership),
      });
    }
  }
  if (userBin !== 'absent') {
    kept.push({
      path: userBinPath(ctx),
      reason:
        userBin === 'managed'
          ? `A hermes command that this app did not install: it links into ` +
            `${runtimeRoot(ctx)}, which is what hermes's own installer does — ` +
            `this app never writes to ${userBinPath(ctx)}. It is left exactly ` +
            `as it is.`
          : 'A hermes command that this app did not install. It is left exactly as it is.',
    });
  }
  const ownCliConfig = join(goosarHomeDir(ctx), 'config.json');
  if (await pathExists(ownCliConfig)) {
    kept.push({
      path: ownCliConfig,
      reason: 'Your own goosar CLI configuration, not created by this app.',
    });
  }
  for (const path of foreignProfiles) {
    kept.push({
      path,
      reason: 'A goosar CLI profile you configured yourself.',
    });
  }
  return kept;
}

export async function planUninstall(ctx: UninstallContext): Promise<UninstallPlan> {
  const allProfiles = await profileDirs(ctx);
  const ownProfiles = allProfiles.filter((path) =>
    basename(path).startsWith(DESKTOP_PROFILE_PREFIX),
  );
  const foreignProfiles = allProfiles.filter(
    (path) => !basename(path).startsWith(DESKTOP_PROFILE_PREFIX),
  );

  const userBin = await classifyUserBinForPlatform(ctx);
  const ownership = await classifyRuntimeOwnership(ctx);
  const software = softwareCandidates(ctx, ownProfiles, ownership === 'managed');

  return {
    software: await materialize(software),
    userData: await materialize(userDataCandidates(ctx)),
    kept: await keptItems(ctx, userBin, foreignProfiles, ownership),
    manualSteps: manualSteps(ctx),
  };
}

function removableRoots(ctx: UninstallContext): string[] {
  return [agentHomeDir(ctx), goosarHomeDir(ctx), ctx.userDataDir];
}

function isInside(root: string, path: string): boolean {
  return path === root || path.startsWith(root.endsWith(sep) ? root : root + sep);
}

function assertRemovable(ctx: UninstallContext, path: string): void {
  if (path === ctx.home || path === sep) {
    throw new Error(`refusing to remove ${path}: it is the user's own directory`);
  }
  if (!removableRoots(ctx).some((root) => isInside(root, path))) {
    throw new Error(`refusing to remove ${path}: it is outside everything this app installs`);
  }
}

async function removeOne(
  ctx: UninstallContext,
  item: UninstallItem,
): Promise<UninstallFailure | null> {
  try {
    assertRemovable(ctx, item.path);
    await fs.rm(item.path, { recursive: true, force: true });
    if (await pathExists(item.path)) {
      throw new Error('the path is still on disk after the delete returned');
    }
    return null;
  } catch (err) {
    return {
      item,
      message: err instanceof Error ? err.message : String(err),
    };
  }
}

async function pruneIfEmpty(path: string): Promise<void> {
  try {
    await fs.rmdir(path);
  } catch {
    // Not empty, not there, or not ours to remove — all fine.
  }
}

async function stopDaemonSafely(ctx: UninstallContext): Promise<DaemonStopReport> {
  try {
    return await ctx.stopDaemon();
  } catch (err) {
    return {
      stopped: false,
      detail: err instanceof Error ? err.message : String(err),
    };
  }
}

export async function performUninstall(
  ctx: UninstallContext,
  options: UninstallOptions,
): Promise<UninstallOutcome> {
  const plan = await planUninstall(ctx);

  const daemon = await stopDaemonSafely(ctx);
  if (!daemon.stopped) {
    return {
      status: 'blocked',
      daemon,
      removed: [],
      failed: [],
      kept: [
        ...plan.kept,
        ...[...plan.software, ...plan.userData].map((item) => ({
          path: item.path,
          reason:
            'Nothing was removed: the daemon is still running and these are the files it has open.',
        })),
      ],
      manualSteps: plan.manualSteps,
    };
  }

  const kept: KeptItem[] = [...plan.kept];
  if (!options.includeUserData) {
    for (const item of plan.userData) {
      kept.push({
        path: item.path,
        reason: `${item.label} — kept, because you did not ask for your data to be deleted.`,
      });
    }
  }

  const targets = options.includeUserData ? [...plan.software, ...plan.userData] : plan.software;

  const removed: UninstallItem[] = [];
  const failed: UninstallFailure[] = [];
  for (const item of targets) {
    const failure = await removeOne(ctx, item);
    if (failure) failed.push(failure);
    else removed.push(item);
  }

  await pruneIfEmpty(join(goosarHomeDir(ctx), 'profiles'));
  await pruneIfEmpty(goosarHomeDir(ctx));
  await pruneIfEmpty(agentHomeDir(ctx));

  const status =
    failed.length > 0 ? 'partial' : targets.length === 0 ? 'nothing_to_remove' : 'removed';

  return { status, daemon, removed, failed, kept, manualSteps: plan.manualSteps };
}
