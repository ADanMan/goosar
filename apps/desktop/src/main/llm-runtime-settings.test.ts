import { describe, expect, it, vi } from 'vitest';

import {
  buildLlmRuntimePatch,
  isValidApiBase,
  parseLlmRuntimeSaveInput,
  saveLlmRuntimeSettings,
  type LlmRuntimeSaveDeps,
} from './llm-runtime-settings';
import type { AgentConfigPatch, AgentConfigSaveResult } from '../shared/agent-runtime-types';
import type { HttpReachability } from '../shared/perimeter-config';

const GATEWAY = 'https://gateway.example/v1';
const MODEL = 'openai/glm-4.6';

const OK_SAVE: AgentConfigSaveResult = {
  ok: true,
  path: '/home/u/.hermes/config.user.yaml',
  shadowed: [],
};

const REACHABLE: HttpReachability = { kind: 'response', status: 200 };

function makeDeps(overrides: Partial<LlmRuntimeSaveDeps> = {}): {
  deps: LlmRuntimeSaveDeps;
  written: AgentConfigPatch[];
  probes: number;
} {
  const written: AgentConfigPatch[] = [];
  const counter = { probes: 0 };
  const deps: LlmRuntimeSaveDeps = {
    saveConfig:
      overrides.saveConfig ??
      vi.fn(async (patch: AgentConfigPatch) => {
        written.push(patch);
        return OK_SAVE;
      }),
    probeReachability:
      overrides.probeReachability ??
      vi.fn(async () => {
        counter.probes += 1;
        return REACHABLE;
      }),
  };
  return {
    deps,
    written,
    get probes() {
      return counter.probes;
    },
  };
}

describe('buildLlmRuntimePatch', () => {
  it('writes only api_base and model, never the api key', () => {
    const patch = buildLlmRuntimePatch({ apiBase: GATEWAY, model: MODEL });
    expect(patch).toEqual({ 'llm.api_base': GATEWAY, 'llm.model': MODEL });
    expect(patch).not.toHaveProperty('llm.api_key');
  });

  it('trims pasted whitespace off both fields', () => {
    const patch = buildLlmRuntimePatch({
      apiBase: `  ${GATEWAY}\n`,
      model: `\t${MODEL} `,
    });
    expect(patch).toEqual({ 'llm.api_base': GATEWAY, 'llm.model': MODEL });
  });

  it('is idempotent — the same input yields the same patch', () => {
    const input = { apiBase: GATEWAY, model: MODEL };
    expect(buildLlmRuntimePatch(input)).toEqual(buildLlmRuntimePatch(input));
  });
});

describe('isValidApiBase', () => {
  it('accepts http and https URLs', () => {
    expect(isValidApiBase('https://gateway.example/v1')).toBe(true);
    expect(isValidApiBase('http://localhost:1234/v1')).toBe(true);
  });

  it('rejects a bare host, empty string, and non-http schemes', () => {
    expect(isValidApiBase('gateway.example')).toBe(false);
    expect(isValidApiBase('')).toBe(false);
    expect(isValidApiBase('ftp://gateway.example')).toBe(false);
    expect(isValidApiBase('file:///etc/passwd')).toBe(false);
  });
});

describe('parseLlmRuntimeSaveInput', () => {
  it('coerces missing or non-string fields to empty strings', () => {
    expect(parseLlmRuntimeSaveInput(null)).toEqual({ apiBase: '', model: '' });
    expect(parseLlmRuntimeSaveInput({ apiBase: 42, model: {} })).toEqual({
      apiBase: '',
      model: '',
    });
    expect(parseLlmRuntimeSaveInput({ apiBase: GATEWAY, model: MODEL, extra: 'x' })).toEqual({
      apiBase: GATEWAY,
      model: MODEL,
    });
  });
});

describe('saveLlmRuntimeSettings', () => {
  it('writes only api_base + model through the config chokepoint, then probes', async () => {
    const h = makeDeps();
    const result = await saveLlmRuntimeSettings(h.deps, {
      apiBase: GATEWAY,
      model: MODEL,
    });

    expect(h.written).toEqual([{ 'llm.api_base': GATEWAY, 'llm.model': MODEL }]);
    expect(h.written[0]).not.toHaveProperty('llm.api_key');
    expect(h.probes).toBe(1);
    expect(result).toEqual({
      status: 'written',
      saved: OK_SAVE,
      reachability: REACHABLE,
    });
  });

  it('surfaces an unreachable endpoint after a successful write', async () => {
    const h = makeDeps({
      probeReachability: vi.fn(async () => ({ kind: 'unreachable' as const })),
    });
    const result = await saveLlmRuntimeSettings(h.deps, {
      apiBase: GATEWAY,
      model: MODEL,
    });
    expect(result).toEqual({
      status: 'written',
      saved: OK_SAVE,
      reachability: { kind: 'unreachable' },
    });
  });

  it('does not probe when the config write itself failed', async () => {
    const failed: AgentConfigSaveResult = {
      ok: false,
      field: 'llm.api_base',
      kind: 'invalid_value',
      message: 'hermes rejected that value.',
    };
    const probeReachability = vi.fn(async () => REACHABLE);
    const result = await saveLlmRuntimeSettings(
      makeDeps({ saveConfig: vi.fn(async () => failed), probeReachability }).deps,
      { apiBase: GATEWAY, model: MODEL },
    );

    expect(probeReachability).not.toHaveBeenCalled();
    expect(result).toEqual({
      status: 'written',
      saved: failed,
      reachability: null,
    });
  });

  it('refuses a blank model or endpoint without writing', async () => {
    const saveConfig = vi.fn();
    const deps = makeDeps({ saveConfig }).deps;

    const blankModel = await saveLlmRuntimeSettings(deps, {
      apiBase: GATEWAY,
      model: '  ',
    });
    const blankBase = await saveLlmRuntimeSettings(deps, {
      apiBase: '',
      model: MODEL,
    });

    expect(saveConfig).not.toHaveBeenCalled();
    expect(blankModel).toEqual({
      status: 'rejected',
      rejection: { reason: 'empty', message: expect.any(String) },
    });
    expect(blankBase.status).toBe('rejected');
  });

  it('refuses an invalid endpoint URL without writing', async () => {
    const saveConfig = vi.fn();
    const result = await saveLlmRuntimeSettings(makeDeps({ saveConfig }).deps, {
      apiBase: 'gateway.example',
      model: MODEL,
    });
    expect(saveConfig).not.toHaveBeenCalled();
    expect(result).toEqual({
      status: 'rejected',
      rejection: { reason: 'invalid_api_base', message: expect.any(String) },
    });
  });
});
