import { describe, expect, it } from 'vitest';
import { EMPTY_DEPLOYMENT_HOSTS } from '@goosar/core/config';
import {
  getLlmConnectionPreset,
  llmConnectionPresets,
  LLM_CONNECTION_PRESET_IDS,
} from './llm-presets';

const CONFIGURED = {
  ...EMPTY_DEPLOYMENT_HOSTS,
  llmApiBase: 'https://llm.example.test/api/v3',
  llmModel: 'openai/coding-medium',
};

describe('llmConnectionPresets', () => {
  it('offers the perimeter gateway the deployment named, in display order', () => {
    expect(llmConnectionPresets(CONFIGURED).map((p) => p.id)).toEqual(['perimeter', 'outside']);
  });

  it('omits the perimeter preset when the deployment named no gateway', () => {
    expect(llmConnectionPresets(EMPTY_DEPLOYMENT_HOSTS).map((p) => p.id)).toEqual(['outside']);
  });

  it('carries no secret-shaped fields — endpoint and model only', () => {
    for (const preset of llmConnectionPresets(CONFIGURED)) {
      expect(Object.keys(preset).sort()).toEqual(['apiBase', 'id', 'model']);
    }
  });
});

describe('getLlmConnectionPreset', () => {
  it('takes the perimeter endpoint and model from deployment defaults', () => {
    expect(getLlmConnectionPreset('perimeter', CONFIGURED)).toEqual({
      id: 'perimeter',
      apiBase: 'https://llm.example.test/api/v3',
      model: 'openai/coding-medium',
    });
  });

  it('falls back to the default model when only a base URL is configured', () => {
    expect(
      getLlmConnectionPreset('perimeter', {
        ...EMPTY_DEPLOYMENT_HOSTS,
        llmApiBase: 'https://llm.example.test/v1',
      }),
    ).toEqual({
      id: 'perimeter',
      apiBase: 'https://llm.example.test/v1',
      model: 'openai/coding-medium',
    });
  });

  it('returns null rather than a placeholder address when unconfigured', () => {
    expect(getLlmConnectionPreset('perimeter', EMPTY_DEPLOYMENT_HOSTS)).toBeNull();
  });

  it('pins the hosted gateway, which ships with the product', () => {
    expect(getLlmConnectionPreset('outside', EMPTY_DEPLOYMENT_HOSTS)).toEqual({
      id: 'outside',
      apiBase: 'https://llm.example.com/v1',
      model: 'openai/glm-5.1',
    });
  });

  it('knows exactly two preset ids', () => {
    expect([...LLM_CONNECTION_PRESET_IDS]).toEqual(['perimeter', 'outside']);
  });
});
