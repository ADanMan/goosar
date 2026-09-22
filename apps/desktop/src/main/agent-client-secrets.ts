import { createHash } from 'crypto';

import { isPlaceholderApiBase, isPlaceholderApiKey, isPlaceholderModel } from './agent-bootstrap';
import {
  getAgentConfigValue,
  saveAgentConfig,
  type AgentCommandRunner,
  type AgentPathContext,
} from './agent-config';
import { readConfigFieldsFromFile, writeLlmFieldsDirectly } from './agent-config-yaml-fallback';
import type { AgentConfigPatch } from '../shared/agent-runtime-types';
import type { AuthedFetch, ProvisioningAuth } from './provisioning';

const LLM_FIELDS = ['llm.api_base', 'llm.model', 'llm.api_key'] as const;

interface DeploymentClientSecretsLLM {
  api_base: string;
  model: string;
  api_key: string;
}

interface DeploymentClientSecretsResponse {
  llm: DeploymentClientSecretsLLM | null;
}

export type ClientSecretsFetchResult =
  | { kind: 'ok'; llm: DeploymentClientSecretsLLM | null }
  /** 403 — this runtime is not managed, or the caller is not authenticated. */
  | { kind: 'forbidden' }
  /** Network error, non-2xx/403 status, or a malformed body. */
  | { kind: 'error'; detail: string };

function isValidLLM(v: unknown): v is DeploymentClientSecretsLLM {
  if (v === null || typeof v !== 'object') return false;
  const r = v as Record<string, unknown>;
  return (
    typeof r.api_base === 'string' && typeof r.model === 'string' && typeof r.api_key === 'string'
  );
}

export async function fetchDeploymentClientSecrets(
  auth: ProvisioningAuth,
  fetchImpl: AuthedFetch,
): Promise<ClientSecretsFetchResult> {
  const url = `${auth.apiBaseUrl.replace(/\/+$/, '')}/api/deployment/client-secrets`;
  let res: Response;
  try {
    res = await fetchImpl(url, {
      headers: {
        Authorization: `Bearer ${auth.token}`,
        'X-Workspace-ID': auth.workspaceId,
        'X-Goosar-Launched-By': 'desktop',
      },
    });
  } catch (err) {
    return {
      kind: 'error',
      detail: err instanceof Error ? err.message : String(err),
    };
  }
  if (res.status === 403) return { kind: 'forbidden' };
  if (!res.ok) return { kind: 'error', detail: `HTTP ${res.status}` };

  let data: unknown;
  try {
    data = await res.json();
  } catch (err) {
    return {
      kind: 'error',
      detail: `malformed response: ${err instanceof Error ? err.message : String(err)}`,
    };
  }
  if (data === null || typeof data !== 'object') {
    return { kind: 'error', detail: 'malformed response: not an object' };
  }
  const body = data as DeploymentClientSecretsResponse;
  if (body.llm !== null && !isValidLLM(body.llm)) {
    return { kind: 'error', detail: 'malformed response: llm block' };
  }
  return { kind: 'ok', llm: body.llm };
}

function isMaskedValue(value: string): boolean {
  return value === '' || /^[*•]+$/.test(value);
}

export function sha256Hex(value: string): string {
  return createHash('sha256').update(value, 'utf-8').digest('hex');
}

const MODEL_PROVIDER_PREFIX = /^[a-z0-9_-]+\//;

export function toAgentModelId(model: string): string {
  if (!model || MODEL_PROVIDER_PREFIX.test(model)) return model;
  return `openai/${model}`;
}

export interface ClientSecretsSyncState {
  issuedApiKeyHash: string | null;
}

export const EMPTY_CLIENT_SECRETS_SYNC_STATE: ClientSecretsSyncState = {
  issuedApiKeyHash: null,
};

export interface ClientSecretsSyncResult {
  note: string;
  fieldsWritten: (typeof LLM_FIELDS)[number][];
  nextState: ClientSecretsSyncState;
  deploymentApiBase: string | null;
}

export async function syncDeploymentClientSecrets(
  ctx: AgentPathContext,
  auth: ProvisioningAuth,
  fetchImpl: AuthedFetch,
  run?: AgentCommandRunner,
  state: ClientSecretsSyncState = EMPTY_CLIENT_SECRETS_SYNC_STATE,
): Promise<ClientSecretsSyncResult> {
  const fetched = await fetchDeploymentClientSecrets(auth, fetchImpl);
  if (fetched.kind === 'forbidden') {
    return {
      note: 'секреты деплоя недоступны: этот рантайм не помечен как управляемый приложением',
      fieldsWritten: [],
      nextState: state,
      deploymentApiBase: null,
    };
  }
  if (fetched.kind === 'error') {
    return {
      note: `секреты деплоя недоступны: ${fetched.detail}`,
      fieldsWritten: [],
      nextState: state,
      deploymentApiBase: null,
    };
  }
  if (!fetched.llm) {
    return {
      note: 'деплой не сообщает LLM-ключ — нечего записывать',
      fieldsWritten: [],
      nextState: state,
      deploymentApiBase: null,
    };
  }
  const llm = fetched.llm;

  const patch: AgentConfigPatch = {};
  const currentBase = await getAgentConfigValue(ctx, 'llm.api_base', run);
  if ((!currentBase || isPlaceholderApiBase(currentBase)) && llm.api_base) {
    patch['llm.api_base'] = llm.api_base;
  }

  const currentModel = await getAgentConfigValue(ctx, 'llm.model', run);
  if ((!currentModel || isPlaceholderModel(currentModel)) && llm.model) {
    patch['llm.model'] = toAgentModelId(llm.model);
  }

  const currentKey = readConfigFieldsFromFile(ctx, ['llm.api_key'])['llm.api_key'];
  const currentKeyIsKnown = currentKey !== null && !isMaskedValue(currentKey);
  const keyIsPlaceholder = currentKeyIsKnown && isPlaceholderApiKey(currentKey as string);
  const keyIsOurOwnPriorIssue =
    currentKeyIsKnown &&
    state.issuedApiKeyHash !== null &&
    sha256Hex(currentKey as string) === state.issuedApiKeyHash;
  const keyIsUnknownAndNeverIssued = !currentKeyIsKnown && state.issuedApiKeyHash === null;
  if (
    llm.api_key &&
    llm.api_key !== currentKey &&
    (keyIsPlaceholder || keyIsOurOwnPriorIssue || keyIsUnknownAndNeverIssued)
  ) {
    patch['llm.api_key'] = llm.api_key;
  }

  const fieldsToAttempt = LLM_FIELDS.filter((f) => patch[f] !== undefined);
  if (fieldsToAttempt.length === 0) {
    return {
      note: 'секреты деплоя уже записаны, изменений нет',
      fieldsWritten: [],
      nextState: state,
      deploymentApiBase: llm.api_base,
    };
  }

  const saveResult = await saveAgentConfig(ctx, patch, run);
  let usedYamlFallback = false;
  if (!saveResult.ok && isValidationFailureOutsideLlm(saveResult.message)) {
    const fallback = await writeLlmFieldsDirectly(ctx, patch);
    if (fallback.ok) {
      usedYamlFallback = true;
    } else {
      return {
        note: `не удалось записать секреты деплоя: ${fallback.message}`,
        fieldsWritten: [],
        nextState: state,
        deploymentApiBase: llm.api_base,
      };
    }
  } else if (!saveResult.ok) {
    return {
      note: `не удалось записать секреты деплоя: ${saveResult.message}`,
      fieldsWritten: [],
      nextState: state,
      deploymentApiBase: llm.api_base,
    };
  }

  const fieldsWritten: (typeof LLM_FIELDS)[number][] = [];
  if (usedYamlFallback) {
    const fromFile = readConfigFieldsFromFile(ctx, fieldsToAttempt);
    for (const field of fieldsToAttempt) {
      if (fromFile[field] !== null && fromFile[field] === patch[field]) {
        fieldsWritten.push(field);
      }
    }
  } else {
    const apiKeyFromFile = fieldsToAttempt.includes('llm.api_key')
      ? readConfigFieldsFromFile(ctx, ['llm.api_key'])['llm.api_key']
      : null;
    for (const field of fieldsToAttempt) {
      if (field === 'llm.api_key') {
        if (apiKeyFromFile !== null && apiKeyFromFile === patch[field]) {
          fieldsWritten.push(field);
        }
        continue;
      }
      const written = await getAgentConfigValue(ctx, field, run);
      if (written !== null && written === patch[field]) {
        fieldsWritten.push(field);
      }
    }
  }

  if (fieldsWritten.length === 0) {
    return {
      note: usedYamlFallback
        ? 'секреты деплоя не удалось подтвердить после прямой записи в YAML'
        : 'секреты деплоя не удалось подтвердить после записи',
      fieldsWritten: [],
      nextState: state,
      deploymentApiBase: llm.api_base,
    };
  }

  const nextState: ClientSecretsSyncState = fieldsWritten.includes('llm.api_key')
    ? { issuedApiKeyHash: sha256Hex(patch['llm.api_key'] as string) }
    : state;

  return {
    note: usedYamlFallback
      ? `секреты деплоя записаны напрямую в YAML (проверено по файлу): ${fieldsWritten.join(', ')}`
      : `секреты деплоя записаны: ${fieldsWritten.join(', ')}`,
    fieldsWritten,
    nextState,
    deploymentApiBase: llm.api_base,
  };
}

function isValidationFailureOutsideLlm(message: string): boolean {
  return !/llm\.(api_base|model|api_key)/.test(message);
}
