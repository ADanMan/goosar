import type {
  LlmConnectionSaveResult,
  LlmConnectionValues,
  SaveLlmConnection,
} from '@goosar/views/onboarding';
import type { AgentConfigPatch } from '../../../shared/agent-runtime-types';

export const saveLlmConnection: SaveLlmConnection = async (
  values: LlmConnectionValues,
): Promise<LlmConnectionSaveResult> => {
  const api = window.daemonAPI;
  if (!api?.setAgentRuntimeConfig) {
    return { ok: false };
  }

  const patch: AgentConfigPatch = {
    'llm.api_base': values.apiBase,
    'llm.model': values.model,
    'llm.api_key': values.apiKey,
  };

  const result = await api.setAgentRuntimeConfig(patch);
  if (result.ok) {
    return { ok: true, ineffective: result.shadowed.length > 0 };
  }

  return { ok: false, message: result.message };
};
