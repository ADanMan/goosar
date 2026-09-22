import { execFile } from 'child_process';
import { existsSync, promises as fs, readdirSync } from 'fs';
import { join } from 'path';

import { agentHomeDir, type AgentPathContext } from './agent-bootstrap';
import { readBundleManifest, verifyBundledEntry } from './bundle-manifest';

const MCP_SERVERS_MANIFEST = 'MCP-SERVERS-MANIFEST.txt';

export function mcpServersRoot(ctx: AgentPathContext): string {
  return join(agentHomeDir(ctx), 'mcp-servers');
}

export interface McpServersStatus {
  installed: string[];
  staged: string[];
  skippedExisting: string[];
  refused: string[];
}

async function serverDirNames(dir: string): Promise<string[]> {
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

export async function firstConfiguredMcpServer(
  ctx: AgentPathContext,
): Promise<{ name: string; dir: string } | null> {
  const root = mcpServersRoot(ctx);
  const names = await serverDirNames(root);
  if (names.length === 0) return null;
  return { name: names[0], dir: join(root, names[0]) };
}

export async function readMcpServersStatus(
  ctx: AgentPathContext,
): Promise<Pick<McpServersStatus, 'installed'>> {
  return { installed: await serverDirNames(mcpServersRoot(ctx)) };
}

async function stageOneServer(
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
      throw new Error(`could not move staged server into ${target}`);
    }
    console.log(`[mcp-servers] staged ${name} → ${target}`);
    return 'staged';
  } catch (err) {
    await fs.rm(temp, { recursive: true, force: true }).catch(() => {});
    console.warn(`[mcp-servers] could not stage ${name}:`, err);
    return 'failed';
  }
}

export async function ensureBundledMcpServers(
  ctx: AgentPathContext,
  bundledDir: string | null,
): Promise<McpServersStatus> {
  const root = mcpServersRoot(ctx);
  const staged: string[] = [];
  const skippedExisting: string[] = [];
  const refused: string[] = [];

  if (bundledDir) {
    const names = await serverDirNames(bundledDir);
    if (names.length > 0) {
      const manifest = await readBundleManifest(bundledDir, MCP_SERVERS_MANIFEST, 'server');
      try {
        await fs.mkdir(root, { recursive: true });
        for (const name of names) {
          const check = await verifyBundledEntry(bundledDir, name, manifest);
          if (!check.ok) {
            console.error(`[mcp-servers] refusing to stage ${name}: ${check.reason}`);
            refused.push(name);
            continue;
          }
          const outcome = await stageOneServer(bundledDir, name, root);
          if (outcome === 'staged') staged.push(name);
          else if (outcome === 'skipped') skippedExisting.push(name);
        }
      } catch (err) {
        console.warn('[mcp-servers] MCP server materialization failed:', err);
      }
    }
  }

  const { installed } = await readMcpServersStatus(ctx);
  return { installed, staged, skippedExisting, refused };
}

function serverBinSubdirs(platform: NodeJS.Platform): string[] {
  return platform === 'win32'
    ? [join('.venv', 'Scripts'), join('.venv', 'bin'), 'bin']
    : [join('.venv', 'bin'), 'bin'];
}

export function stagedMcpServerBinDirs(
  ctx: AgentPathContext,
  platform: NodeJS.Platform = process.platform,
): string[] {
  try {
    const root = mcpServersRoot(ctx);
    const names = readdirSync(root, { withFileTypes: true })
      .filter((entry) => entry.isDirectory() && !entry.name.startsWith('.'))
      .map((entry) => entry.name)
      .sort();
    const dirs: string[] = [];
    for (const name of names) {
      for (const sub of serverBinSubdirs(platform)) {
        const dir = join(root, name, sub);
        if (existsSync(dir)) dirs.push(dir);
      }
    }
    return dirs;
  } catch {
    return [];
  }
}

export function appendStagedMcpServerPath(
  currentPath: string | undefined,
  ctx: AgentPathContext,
  platform: NodeJS.Platform = process.platform,
): string {
  const base = currentPath ?? '';
  const dirs = stagedMcpServerBinDirs(ctx, platform);
  if (dirs.length === 0) return base;
  const sep = platform === 'win32' ? ';' : ':';
  return base.length > 0 ? `${base}${sep}${dirs.join(sep)}` : dirs.join(sep);
}
