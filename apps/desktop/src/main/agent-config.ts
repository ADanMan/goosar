import { spawn } from 'child_process';
import { existsSync } from 'fs';

import { managedAgentPath, type AgentPathContext } from './agent-bootstrap';
import { writeConfigFieldsDirectly } from './agent-config-yaml-fallback';
import {
  AGENT_BASH_FULL_FIELD,
  AGENT_CONFIG_FIELDS,
  type AgentConfigErrorKind,
  type AgentConfigField,
  type AgentConfigPatch,
  type AgentConfigSaveResult,
} from '../shared/agent-runtime-types';

export type { AgentPathContext };

export interface AgentConfigCommand {
  bin: string;
  args: string[];
  stdin: string;
  env: NodeJS.ProcessEnv;
}

export interface AgentCommandOutcome {
  code: number | null;
  stdout: string;
  stderr: string;
  spawnError?: string;
}

export type AgentCommandRunner = (command: AgentConfigCommand) => Promise<AgentCommandOutcome>;

const KIND_BY_EXIT_CODE: Record<number, AgentConfigErrorKind> = {
  1: 'config_unavailable',
  2: 'bad_usage',
  3: 'bad_key',
  4: 'invalid_value',
  5: 'write_failed',
};

const MESSAGE_BY_KIND: Record<AgentConfigErrorKind, string> = {
  runtime_unavailable:
    'This app has not installed a hermes runtime, so it cannot change its settings.',
  config_unavailable: 'hermes found no config file to write into.',
  bad_usage: 'hermes rejected the request.',
  bad_key: 'hermes does not recognise that setting.',
  invalid_value: 'hermes rejected that value, so it was not written.',
  write_failed: 'hermes could not write its config file.',
  unknown: 'hermes answered in a way this app could not interpret.',
};

const CONFIG_SET_TIMEOUT_MS = 30_000;
const MAX_OUTPUT_BYTES = 64 * 1024;

interface ConfigSetEnvelope {
  ok?: unknown;
  path?: unknown;
  shadowed_by_runtime?: unknown;
  error?: { kind?: unknown; message?: unknown };
}

function parseEnvelope(stdout: string): ConfigSetEnvelope | null {
  const lines = stdout.split('\n').filter((line) => line.trim().length > 0);
  for (let i = lines.length - 1; i >= 0; i -= 1) {
    try {
      const parsed: unknown = JSON.parse(lines[i]);
      if (parsed !== null && typeof parsed === 'object') {
        return parsed as ConfigSetEnvelope;
      }
    } catch {
      // Not this line; keep walking backwards.
    }
  }
  return null;
}

function envelopeMessage(envelope: ConfigSetEnvelope | null): string | null {
  const message = envelope?.error?.message;
  return typeof message === 'string' && message.length > 0 ? message : null;
}

function failure(
  field: AgentConfigField | null,
  kind: AgentConfigErrorKind,
  message?: string | null,
): AgentConfigSaveResult {
  return { ok: false, field, kind, message: message ?? MESSAGE_BY_KIND[kind] };
}

function runManagedHermes(command: AgentConfigCommand): Promise<AgentCommandOutcome> {
  return new Promise((resolve) => {
    let child: ReturnType<typeof spawn>;
    try {
      child = spawn(command.bin, command.args, {
        env: command.env,
        stdio: ['pipe', 'pipe', 'pipe'],
      });
    } catch (err) {
      resolve({
        code: null,
        stdout: '',
        stderr: '',
        spawnError: err instanceof Error ? err.message : String(err),
      });
      return;
    }

    let stdout = '';
    let stderr = '';
    let settled = false;

    const finish = (outcome: AgentCommandOutcome) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      resolve(outcome);
    };

    const timer = setTimeout(() => {
      child.kill('SIGKILL');
      finish({
        code: null,
        stdout,
        stderr,
        spawnError: `timed out after ${CONFIG_SET_TIMEOUT_MS} ms`,
      });
    }, CONFIG_SET_TIMEOUT_MS);

    child.stdout?.setEncoding('utf-8');
    child.stderr?.setEncoding('utf-8');
    child.stdout?.on('data', (chunk: string) => {
      if (stdout.length < MAX_OUTPUT_BYTES) stdout += chunk;
    });
    child.stderr?.on('data', (chunk: string) => {
      if (stderr.length < MAX_OUTPUT_BYTES) stderr += chunk;
    });
    child.on('error', (err: Error) =>
      finish({ code: null, stdout, stderr, spawnError: err.message }),
    );
    child.on('close', (code: number | null) => finish({ code, stdout, stderr }));

    child.stdin?.on('error', () => {});
    child.stdin?.end(command.stdin);
  });
}

export async function getAgentConfigValue(
  ctx: AgentPathContext,
  field: AgentConfigField,
  run: AgentCommandRunner = runManagedHermes,
): Promise<string | null> {
  const bin = managedAgentPath(ctx);
  if (!existsSync(bin)) return null;

  const outcome = await run({
    bin,
    args: ['config', 'get', field],
    stdin: '',
    env: ctx.env,
  });
  if (outcome.spawnError !== undefined || outcome.code !== 0) return null;

  const envelope = parseEnvelope(outcome.stdout) as { value?: unknown } | null;
  if (envelope && typeof envelope.value === 'string') {
    const value = envelope.value.trim();
    return value.length > 0 ? value : null;
  }
  const raw = outcome.stdout.trim();
  return raw.length > 0 ? raw : null;
}

export function sanitizeAgentConfigPatch(input: unknown): AgentConfigPatch | null {
  if (input === null || typeof input !== 'object') return null;
  const record = input as Record<string, unknown>;
  const patch: AgentConfigPatch = {};
  for (const field of AGENT_CONFIG_FIELDS) {
    const value = record[field];
    if (typeof value !== 'string') continue;
    if (value.trim().length === 0) continue;
    patch[field] = value;
  }
  const bashFull = record[AGENT_BASH_FULL_FIELD];
  if (typeof bashFull === 'boolean') {
    patch[AGENT_BASH_FULL_FIELD] = bashFull;
  }
  return Object.keys(patch).length > 0 ? patch : null;
}

export async function saveAgentConfig(
  ctx: AgentPathContext,
  patch: AgentConfigPatch,
  run: AgentCommandRunner = runManagedHermes,
): Promise<AgentConfigSaveResult> {
  const bin = managedAgentPath(ctx);
  if (!existsSync(bin)) {
    return failure(null, 'runtime_unavailable');
  }

  const fields = AGENT_CONFIG_FIELDS.filter((field) => (patch[field] ?? '').trim().length > 0);
  if (fields.length === 0) {
    return failure(null, 'bad_usage', 'There is nothing to save.');
  }

  let path: string | null = null;
  const shadowed: AgentConfigField[] = [];

  for (const field of fields) {
    const outcome = await run({
      bin,
      args: ['config', 'set', field, '--stdin'],
      stdin: (patch[field] ?? '').trim(),
      env: ctx.env,
    });

    if (outcome.spawnError !== undefined) {
      return failure(field, 'runtime_unavailable');
    }

    const envelope = parseEnvelope(outcome.stdout);

    if (outcome.code !== 0) {
      const kind =
        outcome.code === null ? 'unknown' : (KIND_BY_EXIT_CODE[outcome.code] ?? 'unknown');
      return failure(field, kind, envelopeMessage(envelope));
    }

    if (envelope?.ok !== true || typeof envelope.path !== 'string') {
      return failure(field, 'unknown');
    }

    path = envelope.path;
    if (envelope.shadowed_by_runtime === true) shadowed.push(field);
  }

  if (path === null) return failure(null, 'unknown');
  return { ok: true, path, shadowed };
}

export async function ensureBashFullFlag(
  ctx: AgentPathContext,
  run: AgentCommandRunner = runManagedHermes,
): Promise<AgentConfigSaveResult | { ok: true; path: null; skipped: true }> {
  const bin = managedAgentPath(ctx);
  if (!existsSync(bin)) return failure(null, 'runtime_unavailable');

  const getOutcome = await run({
    bin,
    args: ['config', 'get', AGENT_BASH_FULL_FIELD],
    stdin: '',
    env: ctx.env,
  });

  if (getOutcome.spawnError === undefined && getOutcome.code === 0) {
    const value = getOutcome.stdout.trim().toLowerCase();
    if (value === 'true' || value === 'false') {
      return { ok: true, path: null, skipped: true };
    }
  }

  const setOutcome = await run({
    bin,
    args: ['config', 'set', AGENT_BASH_FULL_FIELD, '--stdin'],
    stdin: 'true',
    env: ctx.env,
  });

  if (setOutcome.spawnError !== undefined) {
    return failure(null, 'runtime_unavailable');
  }

  const envelope = parseEnvelope(setOutcome.stdout);
  if (setOutcome.code !== 0) {
    const kind =
      setOutcome.code === null ? 'unknown' : (KIND_BY_EXIT_CODE[setOutcome.code] ?? 'unknown');
    const message = envelopeMessage(envelope);
    if (isValidationFailureOutsideField(message, AGENT_BASH_FULL_FIELD)) {
      const fallback = await writeConfigFieldsDirectly(ctx, {
        [AGENT_BASH_FULL_FIELD]: true,
      });
      if (fallback.ok && fallback.path) {
        return { ok: true, path: fallback.path, shadowed: [] };
      }
      return failure(
        null,
        kind,
        `${message} (fallback also failed: ${fallback.message ?? 'unknown error'})`,
      );
    }
    return failure(null, kind, message);
  }
  if (envelope?.ok !== true || typeof envelope.path !== 'string') {
    return failure(null, 'unknown');
  }
  return { ok: true, path: envelope.path, shadowed: [] };
}

function isValidationFailureOutsideField(message: string | null, field: string): boolean {
  return !message?.includes(field);
}
