import type { AgentConfigPatch, AgentConfigSaveResult } from '../shared/agent-runtime-types';
import type { LlmAuthReachability } from '../shared/perimeter-config';
import type { LlmRuntimeSaveInput, LlmRuntimeSaveResult } from '../shared/llm-runtime-settings';

export function isValidApiBase(value: string): boolean {
  let url: URL;
  try {
    url = new URL(value.trim());
  } catch {
    return false;
  }
  return url.protocol === 'http:' || url.protocol === 'https:';
}

export function buildLlmRuntimePatch(input: LlmRuntimeSaveInput): AgentConfigPatch {
  return {
    'llm.api_base': input.apiBase.trim(),
    'llm.model': input.model.trim(),
  };
}

export function parseLlmRuntimeSaveInput(input: unknown): LlmRuntimeSaveInput {
  const record =
    input !== null && typeof input === 'object' ? (input as Record<string, unknown>) : {};
  return {
    apiBase: typeof record.apiBase === 'string' ? record.apiBase : '',
    model: typeof record.model === 'string' ? record.model : '',
  };
}

export interface LlmRuntimeSaveDeps {
  saveConfig: (patch: AgentConfigPatch) => Promise<AgentConfigSaveResult>;
  probeReachability: () => Promise<LlmAuthReachability>;
}

export async function saveLlmRuntimeSettings(
  deps: LlmRuntimeSaveDeps,
  input: LlmRuntimeSaveInput,
): Promise<LlmRuntimeSaveResult> {
  const apiBase = input.apiBase.trim();
  const model = input.model.trim();

  if (apiBase.length === 0 || model.length === 0) {
    return {
      status: 'rejected',
      rejection: {
        reason: 'empty',
        message: 'Both a model and a gateway address are required.',
      },
    };
  }

  if (!isValidApiBase(apiBase)) {
    return {
      status: 'rejected',
      rejection: {
        reason: 'invalid_api_base',
        message:
          'The gateway address must be a full http(s) URL, for example https://gateway.example/v1.',
      },
    };
  }

  const saved = await deps.saveConfig(buildLlmRuntimePatch({ apiBase, model }));
  const reachability = saved.ok ? await deps.probeReachability() : null;
  return { status: 'written', saved, reachability };
}
