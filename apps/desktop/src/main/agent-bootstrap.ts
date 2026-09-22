import { execFile, spawn, type ChildProcessWithoutNullStreams } from 'child_process';
import { constants as fsConstants, existsSync, promises as fs } from 'fs';
import { join, sep } from 'path';

import {
  AGENT_CONFIG_FIELDS,
  type AgentConfigField,
  type AgentRuntimeStatus,
  type McpClientState,
} from '../shared/agent-runtime-types';
import { MCP_CONTRACT_PROBE, classifyMcpProbeAnswer } from '../../scripts/mcp-contract.mjs';

export interface AgentRuntimeContext {
  home: string;
  env: NodeJS.ProcessEnv;
  bundledArtifactDir: string | null;
  platform: NodeJS.Platform;
}

export type AgentPathContext = Pick<AgentRuntimeContext, 'home' | 'env'>;

const INSTALL_TIMEOUT_MS = 5 * 60_000;
const PROBE_TIMEOUT_MS = 30_000;

export function agentHomeDir(ctx: AgentPathContext): string {
  const override = ctx.env['HERMES_HOME']?.trim();
  return override ? override : join(ctx.home, '.hermes');
}

export function runtimeRoot(ctx: AgentPathContext): string {
  return join(agentHomeDir(ctx), 'runtime');
}

export function managedAgentPath(ctx: AgentPathContext): string {
  return join(runtimeRoot(ctx), 'current', 'bin', 'hermes');
}

export function userBinPath(ctx: AgentPathContext): string {
  const xdgBin = ctx.env['XDG_BIN_HOME']?.trim();
  return join(xdgBin ? xdgBin : join(ctx.home, '.local', 'bin'), 'hermes');
}

function managedBinDir(ctx: AgentPathContext): string {
  return join(runtimeRoot(ctx), 'bin');
}

async function realpathOrSelf(path: string): Promise<string> {
  try {
    return await fs.realpath(path);
  } catch {
    return path;
  }
}

export type UserBinKind = 'absent' | 'managed' | 'foreign';

export async function classifyUserBin(ctx: AgentPathContext): Promise<UserBinKind> {
  const path = userBinPath(ctx);
  let stat: Awaited<ReturnType<typeof fs.lstat>>;
  try {
    stat = await fs.lstat(path);
  } catch {
    return 'absent';
  }
  if (!stat.isSymbolicLink()) return 'foreign';
  try {
    const target = await fs.realpath(path);
    const root = await realpathOrSelf(runtimeRoot(ctx));
    return target === root || target.startsWith(root + sep) ? 'managed' : 'foreign';
  } catch {
    return 'foreign';
  }
}

async function readVersionFile(dir: string): Promise<string | null> {
  try {
    const text = await fs.readFile(join(dir, 'VERSION'), 'utf-8');
    const version = text.trim();
    return version.length > 0 ? version : null;
  } catch {
    return null;
  }
}

export async function bundledAgentVersion(ctx: AgentRuntimeContext): Promise<string | null> {
  if (!ctx.bundledArtifactDir) return null;
  if (!existsSync(join(ctx.bundledArtifactDir, 'bin', 'hermes'))) return null;
  return readVersionFile(ctx.bundledArtifactDir);
}

export async function installedAgentVersion(ctx: AgentPathContext): Promise<string | null> {
  if (!existsSync(managedAgentPath(ctx))) return null;
  return readVersionFile(join(runtimeRoot(ctx), 'current'));
}

export function resolveAgentConfigPath(ctx: AgentPathContext): string | null {
  for (const name of ['HERMES_CONFIG_PATH', 'OPENCLAW_CONFIG_PATH']) {
    const explicit = ctx.env[name]?.trim();
    if (explicit && existsSync(explicit)) return explicit;
  }

  const seeded = join(agentHomeDir(ctx), 'config.user.yaml');
  if (existsSync(seeded)) return seeded;

  const xdg = ctx.env['XDG_CONFIG_HOME']?.trim();
  const base = xdg && xdg.startsWith('/') ? xdg : join(ctx.home, '.config');
  const candidate = join(base, 'hermes', 'config.user.yaml');
  return existsSync(candidate) ? candidate : null;
}

function readLlmScalar(configText: string, field: string): string | null {
  let inLlmBlock = false;
  for (const rawLine of configText.split('\n')) {
    const line = rawLine.replace(/\r$/, '');
    if (/^\s*(#|$)/.test(line)) continue;
    if (!/^\s/.test(line)) {
      inLlmBlock = /^llm:\s*$/.test(line);
      continue;
    }
    if (!inLlmBlock) continue;
    const match = line.match(new RegExp(`^\\s+${field}:\\s*(.*)$`));
    if (!match) continue;
    const value = match[1]
      .trim()
      .replace(/\s+#.*$/, '')
      .trim()
      .replace(/^(['"])(.*)\1$/, '$2')
      .trim();
    return value.length > 0 ? value : null;
  }
  return null;
}

export const PLACEHOLDER_API_KEY = 'REPLACE_WITH_YOUR_KEY';
export const PLACEHOLDER_API_BASE_HOST = 'your-gateway.example';
export const PLACEHOLDER_MODEL = 'openai/glm-4.6';

export function isPlaceholderApiBase(apiBase: string): boolean {
  try {
    return new URL(apiBase).hostname === PLACEHOLDER_API_BASE_HOST;
  } catch {
    return true;
  }
}

export function isPlaceholderModel(model: string): boolean {
  return model === PLACEHOLDER_MODEL;
}

export function isPlaceholderApiKey(apiKey: string): boolean {
  return apiKey === PLACEHOLDER_API_KEY;
}

export interface LlmProfileScalars {
  apiBase: string | null;
  model: string | null;
  apiKey: string | null;
}

export function readLlmProfileScalars(configText: string): LlmProfileScalars {
  return {
    apiBase: readLlmScalar(configText, 'api_base'),
    model: readLlmScalar(configText, 'model'),
    apiKey: readLlmScalar(configText, 'api_key'),
  };
}

export function missingAgentConfigFields(configText: string): AgentConfigField[] {
  const apiKey = readLlmScalar(configText, 'api_key');
  const apiBase = readLlmScalar(configText, 'api_base');
  const model = readLlmScalar(configText, 'model');

  const missing: AgentConfigField[] = [];
  if (apiBase !== null && isPlaceholderApiBase(apiBase)) {
    missing.push('llm.api_base');
  }
  if (model === null) missing.push('llm.model');
  if (apiKey === null || apiKey === PLACEHOLDER_API_KEY) {
    missing.push('llm.api_key');
  }
  return AGENT_CONFIG_FIELDS.filter((field) => missing.includes(field));
}

const WINDOWS_INSTALLER_CANDIDATES = [
  join('bin', 'install-artifact.ps1'),
  join('bin', 'install-artifact.cmd'),
];

function findWindowsInstaller(artifactDir: string): string | null {
  for (const candidate of WINDOWS_INSTALLER_CANDIDATES) {
    const path = join(artifactDir, candidate);
    if (existsSync(path)) return path;
  }
  return null;
}

function windowsRuntimeStatus(ctx: AgentRuntimeContext): AgentRuntimeStatus {
  const artifactDir = ctx.bundledArtifactDir;
  if (!artifactDir) {
    return {
      state: 'not_installed',
      detail:
        'This Windows build carries no hermes runtime — hermes-agent does ' +
        'not publish a Windows artifact yet (ADanMan/hermes-agent#38). ' +
        'Nothing was installed and nothing on this machine was changed.',
    };
  }
  const installer = findWindowsInstaller(artifactDir);
  if (!installer) {
    return {
      state: 'not_installed',
      detail:
        `The runtime bundled at ${artifactDir} carries no Windows installer ` +
        `(looked for ${WINDOWS_INSTALLER_CANDIDATES.join(' and ')}). An ` +
        'artifact installs itself, so nothing was installed.',
    };
  }
  return {
    state: 'not_installed',
    detail:
      `The runtime bundled at ${artifactDir} carries ${installer}, but this ` +
      'app has not been taught its arguments yet, and running an installer ' +
      'with the wrong ones can repoint a hermes you already have. Nothing ' +
      'was installed; see ADanMan/hermes-agent#38.',
  };
}

export async function stripQuarantineAttributes(
  ctx: Pick<AgentRuntimeContext, 'platform' | 'env'>,
  artifactDir: string,
): Promise<{ ok: true } | { ok: false; message: string }> {
  if (ctx.platform !== 'darwin') return { ok: true };
  return new Promise((resolve) => {
    execFile(
      'xattr',
      ['-dr', 'com.apple.quarantine', artifactDir],
      { timeout: PROBE_TIMEOUT_MS, env: ctx.env },
      (err, _stdout, stderr) => {
        if (err) {
          const message = stderr?.trim() || err.message;
          console.warn(
            `[agent-bootstrap] xattr -dr com.apple.quarantine ${artifactDir} failed: ${message}`,
          );
          resolve({ ok: false, message });
          return;
        }
        resolve({ ok: true });
      },
    );
  });
}

function runInstaller(ctx: AgentRuntimeContext, artifactDir: string): Promise<void> {
  const installer = join(artifactDir, 'bin', 'install-artifact.sh');
  const args = [
    installer,
    '--artifact',
    artifactDir,
    '--prefix',
    runtimeRoot(ctx),
    '--bin',
    managedBinDir(ctx),
  ];
  return new Promise((resolve, reject) => {
    execFile(
      '/bin/bash',
      args,
      { timeout: INSTALL_TIMEOUT_MS, env: ctx.env, maxBuffer: 4 * 1024 * 1024 },
      (err, stdout, stderr) => {
        const output = `${stdout ?? ''}${stderr ?? ''}`.trim();
        if (output) console.log(`[agent-bootstrap] installer:\n${output}`);
        if (err) reject(err);
        else resolve();
      },
    );
  });
}

export async function probeInstalledMcpClient(ctx: AgentPathContext): Promise<McpClientState> {
  const python = join(runtimeRoot(ctx), 'current', 'venv', 'bin', 'python');
  if (!existsSync(python)) {
    return {
      state: 'unknown',
      detail: `this runtime has no interpreter at ${python} to ask.`,
    };
  }
  const raw = await new Promise<string | null>((resolve) => {
    execFile(python, ['-c', MCP_CONTRACT_PROBE], { timeout: PROBE_TIMEOUT_MS }, (err, stdout) =>
      resolve(err ? null : stdout),
    );
  });
  if (raw === null) {
    return {
      state: 'unknown',
      detail: `${python} could not be asked about its MCP client.`,
    };
  }
  let answer: unknown;
  try {
    answer = JSON.parse(raw.trim().split('\n').pop() ?? '');
  } catch {
    return {
      state: 'unknown',
      detail: 'the MCP client probe answered in a way we cannot read.',
    };
  }
  const verdict = classifyMcpProbeAnswer(answer as never);
  switch (verdict.state) {
    case 'ok':
      return { state: 'ok', version: verdict.version };
    case 'absent':
      return { state: 'absent', detail: verdict.detail };
    case 'incompatible':
      return {
        state: 'incompatible',
        version: verdict.version,
        reason: verdict.reason,
      };
    default:
      return { state: 'unknown', detail: verdict.reason };
  }
}

export function probeAgentBinary(bin: string): Promise<string | null> {
  return new Promise((resolve) => {
    execFile(bin, ['--version'], { timeout: PROBE_TIMEOUT_MS }, (err, stdout) => {
      if (err) {
        console.warn(`[agent-bootstrap] ${bin} --version failed:`, err.message);
        resolve(null);
        return;
      }
      resolve(stdout.trim());
    });
  });
}

export const BASH_PROBE_TIMEOUT_MS = 30_000;
const BASH_PROBE_MARKER = 'goosar-ok';

export type BashProbeResult = { ok: true } | { ok: false; reason: string };

interface JsonRpcMessage {
  jsonrpc?: unknown;
  id?: unknown;
  method?: unknown;
  params?: unknown;
  result?: unknown;
  error?: { message?: unknown };
}

export function probeBashViaAcp(bin: string, env: NodeJS.ProcessEnv): Promise<BashProbeResult> {
  return new Promise((resolve) => {
    let child: ChildProcessWithoutNullStreams;
    try {
      child = spawn(bin, ['acp'], { env, stdio: ['pipe', 'pipe', 'pipe'] });
    } catch (err) {
      resolve({
        ok: false,
        reason: err instanceof Error ? err.message : String(err),
      });
      return;
    }

    let buffer = '';
    let settled = false;
    let sessionId: string | null = null;

    const finish = (result: BashProbeResult) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      child.kill('SIGKILL');
      resolve(result);
    };

    const timer = setTimeout(() => {
      finish({
        ok: false,
        reason: `no reply within ${BASH_PROBE_TIMEOUT_MS} ms`,
      });
    }, BASH_PROBE_TIMEOUT_MS);

    const send = (msg: Record<string, unknown>) => {
      child.stdin.write(`${JSON.stringify(msg)}\n`);
    };

    child.on('error', (err: Error) => finish({ ok: false, reason: err.message }));
    child.on('close', (code: number | null) => {
      if (!settled) {
        finish({ ok: false, reason: `acp process exited (code ${code})` });
      }
    });

    child.stdout.setEncoding('utf-8');
    child.stdout.on('data', (chunk: string) => {
      buffer += chunk;
      for (let idx = buffer.indexOf('\n'); idx >= 0; idx = buffer.indexOf('\n')) {
        const line = buffer.slice(0, idx).trim();
        buffer = buffer.slice(idx + 1);
        if (!line) continue;
        let msg: JsonRpcMessage;
        try {
          msg = JSON.parse(line) as JsonRpcMessage;
        } catch {
          continue;
        }
        handleMessage(msg);
      }
    });

    function handleMessage(msg: JsonRpcMessage): void {
      if (msg.method === 'session/request_permission') {
        send({
          jsonrpc: '2.0',
          id: msg.id,
          result: { outcome: { outcome: 'selected', optionId: 'allow' } },
        });
        return;
      }
      if (msg.error) {
        finish({
          ok: false,
          reason: typeof msg.error.message === 'string' ? msg.error.message : 'acp error response',
        });
        return;
      }
      if (msg.id === 1) {
        send({
          jsonrpc: '2.0',
          id: 2,
          method: 'session/new',
          params: { cwd: process.cwd(), mcpServers: [] },
        });
        return;
      }
      if (msg.id === 2) {
        const result = msg.result as { sessionId?: unknown } | undefined;
        sessionId = typeof result?.sessionId === 'string' ? result.sessionId : null;
        if (!sessionId) {
          finish({ ok: false, reason: 'session/new returned no sessionId' });
          return;
        }
        send({
          jsonrpc: '2.0',
          id: 3,
          method: 'session/prompt',
          params: {
            sessionId,
            prompt: [
              {
                type: 'text',
                text: `Run bash -c 'echo ${BASH_PROBE_MARKER}' and return the exact output.`,
              },
            ],
          },
        });
        return;
      }
      if (msg.id === 3) {
        const text = JSON.stringify(msg.result ?? '');
        if (text.includes(BASH_PROBE_MARKER)) {
          finish({ ok: true });
        } else {
          finish({
            ok: false,
            reason: `bash output did not contain "${BASH_PROBE_MARKER}"`,
          });
        }
        return;
      }
    }

    send({
      jsonrpc: '2.0',
      id: 1,
      method: 'initialize',
      params: { protocolVersion: 1 },
    });
  });
}

export async function ensureBashProbe(
  ctx: AgentPathContext,
  bin: string,
): Promise<BashProbeResult & { skipped?: boolean }> {
  if (ctx.env['GOOSAR_AGENT_BASH_PROBE'] !== '1') {
    return { ok: false, reason: 'bash probe not enabled', skipped: true };
  }
  return probeBashViaAcp(bin, ctx.env);
}

async function describeInstalled(
  ctx: AgentRuntimeContext,
  extra: { userBinConflict?: string },
): Promise<AgentRuntimeStatus> {
  const binPath = managedAgentPath(ctx);
  const version = (await installedAgentVersion(ctx)) ?? 'unknown';
  const configPath = resolveAgentConfigPath(ctx);
  const mcpClient = await probeInstalledMcpClient(ctx);
  if (!configPath) {
    return {
      state: 'needs_config',
      version,
      binPath,
      configPath: null,
      missing: [...AGENT_CONFIG_FIELDS],
      mcpClient,
      ...extra,
    };
  }
  let missing: AgentConfigField[] = [...AGENT_CONFIG_FIELDS];
  try {
    missing = missingAgentConfigFields(await fs.readFile(configPath, 'utf-8'));
  } catch (err) {
    console.warn('[agent-bootstrap] could not read agent config:', err);
  }
  return missing.length === 0
    ? { state: 'ready', version, binPath, configPath, mcpClient, ...extra }
    : {
        state: 'needs_config',
        version,
        binPath,
        configPath,
        missing,
        mcpClient,
        ...extra,
      };
}

export async function ensureAgentRuntime(ctx: AgentRuntimeContext): Promise<AgentRuntimeStatus> {
  if (ctx.platform === 'win32') return windowsRuntimeStatus(ctx);

  const installed = await installedAgentVersion(ctx);
  const userBin = await classifyUserBin(ctx);
  const conflict = userBin === 'foreign' ? { userBinConflict: userBinPath(ctx) } : {};

  if (!installed && userBin === 'foreign') {
    const path = userBinPath(ctx);
    console.log(
      `[agent-bootstrap] found an existing hermes at ${path} that is not ` +
        'managed by this app — leaving it alone and not installing the ' +
        'bundled runtime.',
    );
    return { state: 'external', path };
  }

  const bundled = await bundledAgentVersion(ctx);

  if (installed && (!bundled || bundled === installed)) {
    console.log(`[agent-bootstrap] hermes ${installed} already installed`);
    return describeInstalled(ctx, conflict);
  }

  if (!bundled) {
    return {
      state: 'not_installed',
      detail:
        'This build does not carry a hermes runtime (see ' +
        'apps/desktop/scripts/bundle-agent.mjs).',
    };
  }

  const artifactDir = ctx.bundledArtifactDir as string;
  console.log(
    installed
      ? `[agent-bootstrap] updating hermes ${installed} → ${bundled}`
      : `[agent-bootstrap] installing hermes ${bundled}`,
  );
  await stripQuarantineAttributes(ctx, artifactDir);

  try {
    await runInstaller(ctx, artifactDir);
  } catch (err) {
    const detail = err instanceof Error ? err.message : String(err);
    console.warn('[agent-bootstrap] install failed:', detail);
    if (installed) return describeInstalled(ctx, conflict);
    return { state: 'not_installed', detail };
  }

  const binPath = managedAgentPath(ctx);
  if ((await probeAgentBinary(binPath)) === null) {
    return {
      state: 'not_installed',
      detail: `installed runtime at ${binPath} did not run`,
    };
  }
  return describeInstalled(ctx, conflict);
}

export async function readAgentRuntimeStatus(
  ctx: AgentRuntimeContext,
): Promise<AgentRuntimeStatus> {
  if (ctx.platform === 'win32') return windowsRuntimeStatus(ctx);
  const installed = await installedAgentVersion(ctx);
  const userBin = await classifyUserBin(ctx);
  if (!installed) {
    if (userBin === 'foreign') {
      return { state: 'external', path: userBinPath(ctx) };
    }
    return {
      state: 'not_installed',
      detail: ctx.bundledArtifactDir
        ? 'The bundled runtime has not been installed yet.'
        : 'This build does not carry a hermes runtime.',
    };
  }
  return describeInstalled(ctx, userBin === 'foreign' ? { userBinConflict: userBinPath(ctx) } : {});
}

export function agentPathEnvValue(status: AgentRuntimeStatus): string | null {
  return status.state === 'ready' || status.state === 'needs_config' ? status.binPath : null;
}

export const CA_BUNDLE_FILENAME = 'ca-bundle.pem';
export const CORP_CA_FILENAME = 'corp-ca.pem';

export function caBundleFilePath(ctx: AgentPathContext): string {
  return join(agentHomeDir(ctx), CA_BUNDLE_FILENAME);
}

export function corpCaFilePath(ctx: AgentPathContext): string {
  return join(agentHomeDir(ctx), CORP_CA_FILENAME);
}

export interface CaBundleStatus {
  caBundlePresent: boolean;
  corpCaPresent: boolean;
}

export function readCaBundleStatus(ctx: AgentPathContext): CaBundleStatus {
  return {
    caBundlePresent: existsSync(caBundleFilePath(ctx)),
    corpCaPresent: existsSync(corpCaFilePath(ctx)),
  };
}

async function materializeCaFile(
  sourceDir: string,
  filename: string,
  target: string,
): Promise<void> {
  const source = join(sourceDir, filename);
  if (!existsSync(source) || existsSync(target)) return;
  try {
    await fs.copyFile(source, target, fsConstants.COPYFILE_EXCL);
    console.log(`[agent-bootstrap] materialized ${filename} → ${target}`);
  } catch (err) {
    console.warn(`[agent-bootstrap] could not materialize ${filename}:`, err);
  }
}

export async function ensureCaBundleFiles(
  ctx: AgentPathContext,
  bundledCaDir: string | null,
): Promise<CaBundleStatus> {
  if (bundledCaDir) {
    try {
      await fs.mkdir(agentHomeDir(ctx), { recursive: true });
      await materializeCaFile(bundledCaDir, CA_BUNDLE_FILENAME, caBundleFilePath(ctx));
      await materializeCaFile(bundledCaDir, CORP_CA_FILENAME, corpCaFilePath(ctx));
    } catch (err) {
      console.warn('[agent-bootstrap] CA bundle materialization failed:', err);
    }
  }
  return readCaBundleStatus(ctx);
}
