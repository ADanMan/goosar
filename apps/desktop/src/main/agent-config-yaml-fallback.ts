import { readFileSync, renameSync, writeFileSync } from 'fs';
import { randomBytes } from 'crypto';
import { dirname, join } from 'path';

import { parseDocument } from 'yaml';

import { resolveAgentConfigPath, type AgentPathContext } from './agent-bootstrap';
import type { AgentConfigPatch } from '../shared/agent-runtime-types';

export interface YamlFallbackResult {
  ok: boolean;
  message?: string;
  path?: string;
}

export async function writeConfigFieldsDirectly(
  ctx: AgentPathContext,
  fields: Record<string, unknown>,
): Promise<YamlFallbackResult> {
  const path = resolveAgentConfigPath(ctx);
  if (!path) {
    return { ok: false, message: 'no config.user.yaml found for this runtime' };
  }

  let text: string;
  try {
    text = readFileSync(path, 'utf-8');
  } catch (err) {
    return {
      ok: false,
      message: `could not read config file: ${err instanceof Error ? err.message : String(err)}`,
    };
  }

  let doc: ReturnType<typeof parseDocument>;
  try {
    doc = parseDocument(text);
    if (doc.errors.length > 0) {
      return { ok: false, message: `config file is not valid YAML: ${doc.errors[0]?.message}` };
    }
  } catch (err) {
    return {
      ok: false,
      message: `config file is not valid YAML: ${err instanceof Error ? err.message : String(err)}`,
    };
  }

  for (const [key, value] of Object.entries(fields)) {
    if (value === undefined) continue;
    doc.setIn(key.split('.'), value);
  }

  const nextText = doc.toString();

  try {
    const tmpPath = join(dirname(path), `.config.user.yaml.${randomBytes(6).toString('hex')}.tmp`);
    writeFileSync(tmpPath, nextText, { mode: 0o600 });
    renameSync(tmpPath, path);
  } catch (err) {
    return {
      ok: false,
      message: `could not write config file: ${err instanceof Error ? err.message : String(err)}`,
    };
  }

  return { ok: true, path };
}

export async function writeLlmFieldsDirectly(
  ctx: AgentPathContext,
  patch: AgentConfigPatch,
): Promise<YamlFallbackResult> {
  const fields: Record<string, unknown> = {};
  for (const [field, value] of Object.entries(patch)) {
    if (typeof value !== 'string') continue;
    fields[field] = value;
  }
  return writeConfigFieldsDirectly(ctx, fields);
}

export function readConfigFieldsFromFile(
  ctx: AgentPathContext,
  fields: readonly string[],
): Record<string, string | null> {
  const result: Record<string, string | null> = {};
  for (const field of fields) result[field] = null;

  const path = resolveAgentConfigPath(ctx);
  if (!path) return result;

  let text: string;
  try {
    text = readFileSync(path, 'utf-8');
  } catch {
    return result;
  }

  let doc: ReturnType<typeof parseDocument>;
  try {
    doc = parseDocument(text);
    if (doc.errors.length > 0) return result;
  } catch {
    return result;
  }

  for (const field of fields) {
    const value: unknown = doc.getIn(field.split('.'));
    if (typeof value === 'string') result[field] = value;
  }
  return result;
}
