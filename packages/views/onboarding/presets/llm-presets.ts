// Пресеты подключения LLM для формы онбординга: пользователь не должен
// вводить base URL и id модели вручную.

import { configStore, type DeploymentHosts } from '@goosar/core/config';

export const LLM_CONNECTION_PRESET_IDS = ['perimeter', 'outside'] as const;

export type LlmConnectionPresetId = (typeof LLM_CONNECTION_PRESET_IDS)[number];

export interface LlmConnectionPreset {
  id: LlmConnectionPresetId;
  apiBase: string;
  model: string;
}

const OUTSIDE_PRESET: LlmConnectionPreset = Object.freeze({
  id: 'outside',
  apiBase: 'https://llm.example.com/v1',
  model: 'openai/glm-5.1',
});

const DEFAULT_PERIMETER_MODEL = 'openai/coding-medium';

export function getLlmConnectionPreset(
  id: LlmConnectionPresetId,
  hosts: DeploymentHosts = configStore.getState().deploymentHosts,
): LlmConnectionPreset | null {
  if (id === 'outside') return OUTSIDE_PRESET;
  const apiBase = hosts.llmApiBase.trim();
  if (!apiBase) return null;
  return {
    id: 'perimeter',
    apiBase,
    model: hosts.llmModel.trim() || DEFAULT_PERIMETER_MODEL,
  };
}

export function llmConnectionPresets(
  hosts: DeploymentHosts = configStore.getState().deploymentHosts,
): LlmConnectionPreset[] {
  const presets: LlmConnectionPreset[] = [];
  for (const id of LLM_CONNECTION_PRESET_IDS) {
    const preset = getLlmConnectionPreset(id, hosts);
    if (preset) presets.push(preset);
  }
  return presets;
}

export type LlmConnectionChoice = LlmConnectionPresetId | 'custom';
