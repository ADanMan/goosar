import { describe, it, expect, afterEach, beforeEach, vi } from 'vitest';
import { useCustomPricingStore } from '@goosar/core/runtimes/custom-pricing-store';
import type { AgentRuntime, RuntimeUsage } from '@goosar/core/types';

import {
  addDaysIso,
  aggregateByWeek,
  aggregateCostByModel,
  collectUnmappedModels,
  computeCostInWindow,
  estimateCost,
  estimateCostBreakdown,
  isModelPriced,
  isSelfHealingRuntime,
  sliceWindow,
  todayIso,
  weekStartIso,
} from './utils';

afterEach(() => {
  useCustomPricingStore.setState({ pricings: {} });
});

const zeroUsage = {
  input_tokens: 0,
  output_tokens: 0,
  cache_read_tokens: 0,
  cache_write_tokens: 0,
};

describe('isSelfHealingRuntime', () => {
  function makeRuntime(overrides: Partial<AgentRuntime>): AgentRuntime {
    return {
      id: 'rt-1',
      workspace_id: 'ws-1',
      daemon_id: null,
      name: 'rt',
      runtime_mode: 'local',
      provider: 'claude',
      launch_header: '',
      status: 'online',
      device_info: '',
      metadata: {},
      owner_id: null,
      visibility: 'private',
      last_seen_at: null,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
      ...overrides,
    };
  }

  it('flags an online local runtime as self-healing', () => {
    expect(isSelfHealingRuntime(makeRuntime({ runtime_mode: 'local', status: 'online' }))).toBe(
      true,
    );
  });

  it('treats an offline local runtime as safe to delete', () => {
    expect(isSelfHealingRuntime(makeRuntime({ runtime_mode: 'local', status: 'offline' }))).toBe(
      false,
    );
  });

  it('treats cloud runtimes as safe to delete regardless of status', () => {
    expect(isSelfHealingRuntime(makeRuntime({ runtime_mode: 'cloud', status: 'online' }))).toBe(
      false,
    );
    expect(isSelfHealingRuntime(makeRuntime({ runtime_mode: 'cloud', status: 'offline' }))).toBe(
      false,
    );
  });
});

describe('estimateCost', () => {
  it('prices the canonical Anthropic Sonnet 4.6 SKU', () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: 'claude-sonnet-4-6',
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(18, 5);
  });

  it('prices a Codex CLI session reporting gpt-5-codex', () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: 'gpt-5-codex',
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
      cache_read_tokens: 2_000_000,
    });
    expect(cost).toBeCloseTo(11.5, 5);
  });

  it('strips dated snapshots before resolving (gpt-5-2025-08-07 → gpt-5)', () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: 'gpt-5-2025-08-07',
      input_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(1.25, 5);
  });

  it('prices a Copilot session reporting claude-opus-4.7 at the official Opus rate', () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: 'claude-opus-4.7',
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(5 + 25, 5);
  });

  it('prices Claude Fable 5 at the Mythos-class tier', () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: 'claude-fable-5',
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
      cache_read_tokens: 1_000_000,
      cache_write_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(10 + 50 + 1 + 12.5, 5);
  });

  it("prices Claude Sonnet 5 at Anthropic's intro $2 / $10 tier", () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: 'claude-sonnet-5',
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
      cache_read_tokens: 1_000_000,
      cache_write_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(2 + 10 + 0.2 + 2.5, 5);
  });

  it('prices the provider-prefixed Anthropic form (anthropic/claude-sonnet-4.6)', () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: 'anthropic/claude-sonnet-4.6',
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(3 + 15, 5);
  });

  it('prices the dated dotted Anthropic form (claude-haiku-4.5-20251001)', () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: 'claude-haiku-4.5-20251001',
      input_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(1, 5);
  });

  it('prices the full provider+dotted+dated form (anthropic/claude-opus-4.7-20251001)', () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: 'anthropic/claude-opus-4.7-20251001',
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(5 + 25, 5);
  });

  it('prices the 1M-context Anthropic tag form (claude-opus-4-7[1m]) at the standard Opus tier', () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: 'claude-opus-4-7[1m]',
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(5 + 25, 5);
    expect(isModelPriced('claude-opus-4-7[1m]')).toBe(true);
  });

  it('prices Opus 5 on the standard Opus tier across its transport spellings', () => {
    for (const model of ['claude-opus-5', 'claude-opus-5[1m]', 'anthropic/claude-opus-5']) {
      expect(
        estimateCost({
          ...zeroUsage,
          model,
          input_tokens: 1_000_000,
          output_tokens: 1_000_000,
        }),
      ).toBeCloseTo(5 + 25, 5);
      expect(isModelPriced(model)).toBe(true);
    }
  });

  it('prices each dotted Codex catalog SKU at its own tier, not gpt-5', () => {
    expect(estimateCost({ ...zeroUsage, model: 'gpt-5.5', input_tokens: 1_000_000 })).toBeCloseTo(
      5,
      5,
    );
    expect(estimateCost({ ...zeroUsage, model: 'gpt-5.4', output_tokens: 1_000_000 })).toBeCloseTo(
      15,
      5,
    );
    expect(
      estimateCost({
        ...zeroUsage,
        model: 'gpt-5.4-mini',
        input_tokens: 1_000_000,
        output_tokens: 1_000_000,
      }),
    ).toBeCloseTo(0.75 + 4.5, 5);
    expect(
      estimateCost({
        ...zeroUsage,
        model: 'gpt-5.3-codex',
        input_tokens: 1_000_000,
        output_tokens: 1_000_000,
      }),
    ).toBeCloseTo(1.75 + 14, 5);
  });

  it("prices the gpt-5.6 series per OpenAI's official cache-aware rates", () => {
    const cases = [
      {
        model: 'gpt-5.6-sol',
        input: 5,
        cacheRead: 0.5,
        cacheWrite: 6.25,
        output: 30,
        total: 41.75,
      },
      {
        model: 'gpt-5.6-terra',
        input: 2.5,
        cacheRead: 0.25,
        cacheWrite: 3.125,
        output: 15,
        total: 20.875,
      },
      { model: 'gpt-5.6-luna', input: 1, cacheRead: 0.1, cacheWrite: 1.25, output: 6, total: 8.35 },
    ];
    for (const c of cases) {
      const breakdown = estimateCostBreakdown({
        ...zeroUsage,
        model: c.model,
        input_tokens: 1_000_000,
        cache_read_tokens: 1_000_000,
        cache_write_tokens: 1_000_000,
        output_tokens: 1_000_000,
      });
      expect(breakdown.input).toBeCloseTo(c.input, 5);
      expect(breakdown.cacheRead).toBeCloseTo(c.cacheRead, 5);
      expect(breakdown.cacheWrite).toBeCloseTo(c.cacheWrite, 5);
      expect(breakdown.output).toBeCloseTo(c.output, 5);
      expect(
        estimateCost({
          ...zeroUsage,
          model: c.model,
          input_tokens: 1_000_000,
          cache_read_tokens: 1_000_000,
          cache_write_tokens: 1_000_000,
          output_tokens: 1_000_000,
        }),
      ).toBeCloseTo(c.total, 5);
    }
  });

  it('flags catalog SKUs without a published price (gpt-5.5-mini) as unmapped', () => {
    expect(isModelPriced('gpt-5.5-mini')).toBe(false);
    expect(
      estimateCost({
        ...zeroUsage,
        model: 'gpt-5.5-mini',
        input_tokens: 1_000_000,
      }),
    ).toBe(0);
  });

  it("flags hypothetical future variants as unmapped instead of inheriting a relative's price", () => {
    expect(isModelPriced('gpt-5.99-codex')).toBe(false);
    expect(isModelPriced('gpt-5-foo')).toBe(false);
    expect(isModelPriced('gpt-5-6-luna')).toBe(false);
    expect(isModelPriced('gpt-5-6-sol')).toBe(false);
    expect(
      estimateCost({
        ...zeroUsage,
        model: 'gpt-5.99-codex',
        input_tokens: 1_000_000,
      }),
    ).toBe(0);
  });

  it('returns 0 for a genuinely unknown model so the UI can flag it', () => {
    expect(
      estimateCost({
        ...zeroUsage,
        model: 'totally-made-up-model',
        input_tokens: 1_000_000,
      }),
    ).toBe(0);
  });

  it('prices Cursor Composer rows at the published rates without cache-write spend', () => {
    const costWithAllTokenTypes = (model: string) =>
      estimateCost({
        ...zeroUsage,
        provider: 'runtime-g',
        model,
        input_tokens: 1_000_000,
        output_tokens: 1_000_000,
        cache_read_tokens: 1_000_000,
        cache_write_tokens: 1_000_000,
      });

    expect(costWithAllTokenTypes('auto')).toBeCloseTo(1.25 + 6 + 0.25, 5);
    expect(costWithAllTokenTypes('composer-2.5-fast')).toBeCloseTo(3 + 15 + 0.5, 5);
    expect(costWithAllTokenTypes('composer-2.5')).toBeCloseTo(0.5 + 2.5 + 0.2, 5);
    expect(costWithAllTokenTypes('composer-2-fast')).toBeCloseTo(1.5 + 7.5 + 0.35, 5);
    expect(costWithAllTokenTypes('composer-2')).toBeCloseTo(0.5 + 2.5 + 0.2, 5);
    expect(costWithAllTokenTypes('composer-1.5')).toBeCloseTo(3.5 + 17.5 + 0.35, 5);
    expect(costWithAllTokenTypes('composer-1')).toBeCloseTo(1.25 + 10 + 0.125, 5);
    expect(costWithAllTokenTypes('cursor')).toBeCloseTo(3 + 15 + 0.5, 5);
  });

  it("scopes the generic `auto` id by provider so collisions don't borrow a price", () => {
    const auto = (provider?: string) =>
      estimateCost({ ...zeroUsage, provider, model: 'auto', input_tokens: 1_000_000 });

    expect(auto('runtime-g')).toBeCloseTo(1.25, 5);
    expect(auto('acme')).toBe(0);
    expect(auto(undefined)).toBe(0);
  });

  it('reports provider-qualified keys for unmapped generic model ids', () => {
    const unmapped = collectUnmappedModels([
      { ...zeroUsage, provider: 'acme', model: 'auto' },
      { ...zeroUsage, provider: 'runtime-g', model: 'auto' },
    ]);
    expect(unmapped).toEqual(['acme/auto']);
  });

  it('prices deepseek-v4-flash at the official $0.14/$0.28 with ~50× cache-hit discount', () => {
    const cost = estimateCost({
      ...zeroUsage,
      model: 'deepseek-v4-flash',
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
      cache_read_tokens: 1_000_000,
    });
    expect(cost).toBeCloseTo(0.14 + 0.28 + 0.0028, 5);
  });

  it('prices the deepseek-chat / deepseek-reasoner aliases at the same rate as deepseek-v4-flash', () => {
    const flash = estimateCost({
      ...zeroUsage,
      model: 'deepseek-v4-flash',
      input_tokens: 1_000_000,
    });
    expect(
      estimateCost({
        ...zeroUsage,
        model: 'deepseek-chat',
        input_tokens: 1_000_000,
      }),
    ).toBeCloseTo(flash, 5);
    expect(
      estimateCost({
        ...zeroUsage,
        model: 'deepseek-reasoner',
        input_tokens: 1_000_000,
      }),
    ).toBeCloseTo(flash, 5);
  });

  it('prices kimi-k2.6 at the official $0.95 / $4.00 tier (not the K2 tier)', () => {
    expect(
      estimateCost({
        ...zeroUsage,
        model: 'kimi-k2.6',
        input_tokens: 1_000_000,
        output_tokens: 1_000_000,
      }),
    ).toBeCloseTo(4.95, 5);
  });

  it('prices glm-5.1 at the official $1.4 / $4.4 tier', () => {
    expect(
      estimateCost({
        ...zeroUsage,
        model: 'glm-5.1',
        input_tokens: 1_000_000,
        output_tokens: 1_000_000,
      }),
    ).toBeCloseTo(1.4 + 4.4, 5);
  });

  it('prices glm-4.5-flash at the official Free tier ($0)', () => {
    expect(isModelPriced('glm-4.5-flash')).toBe(true);
    expect(isModelPriced('glm-4.7-flash')).toBe(true);
    expect(
      estimateCost({
        ...zeroUsage,
        model: 'glm-4.5-flash',
        input_tokens: 1_000_000,
        output_tokens: 1_000_000,
      }),
    ).toBe(0);
  });

  it("prices grok-4.5 at xAI's short-context $2.00 / $6.00 tier", () => {
    expect(
      estimateCost({
        ...zeroUsage,
        provider: 'xai',
        model: 'grok-4.5',
        input_tokens: 1_000_000,
        output_tokens: 1_000_000,
        cache_read_tokens: 1_000_000,
      }),
    ).toBeCloseTo(8.3, 5);
  });

  it('prices the rest of the published Grok catalog', () => {
    for (const model of [
      'grok-4.3',
      'grok-4.20-multi-agent-0309',
      'grok-4.20-0309-reasoning',
      'grok-4.20-0309-non-reasoning',
    ]) {
      expect(
        estimateCost({
          ...zeroUsage,
          provider: 'xai',
          model,
          input_tokens: 1_000_000,
          output_tokens: 1_000_000,
        }),
      ).toBeCloseTo(3.75, 5);
    }
    expect(
      estimateCost({
        ...zeroUsage,
        provider: 'xai',
        model: 'grok-build-0.1',
        input_tokens: 1_000_000,
        output_tokens: 1_000_000,
      }),
    ).toBeCloseTo(3, 5);
  });

  it("uses the provider's own cost instead of the rate table when it reports one", () => {
    expect(
      estimateCost({
        ...zeroUsage,
        provider: 'grok',
        model: 'grok-4.5',
        input_tokens: 2049,
        cache_read_tokens: 10880,
        output_tokens: 29,
        cost_usd_ticks: 75_360_000,
        uncosted_input_tokens: 0,
        uncosted_output_tokens: 0,
        uncosted_cache_read_tokens: 0,
        uncosted_cache_write_tokens: 0,
      }),
    ).toBeCloseTo(0.007536, 10);
  });

  it('keeps the long-context surcharge the rate table cannot express', () => {
    const tokens = {
      ...zeroUsage,
      provider: 'grok',
      model: 'grok-4.5',
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
    };
    const shortContext = estimateCost(tokens);
    expect(shortContext).toBeCloseTo(8, 5);

    const longContext = estimateCost({
      ...tokens,
      cost_usd_ticks: 16 * 10_000_000_000, // $16 — the 2x tier
      uncosted_input_tokens: 0,
      uncosted_output_tokens: 0,
      uncosted_cache_read_tokens: 0,
      uncosted_cache_write_tokens: 0,
    });
    expect(longContext).toBeCloseTo(16, 5);
  });

  it('adds an estimate for the tokens the provider did not price', () => {
    expect(
      estimateCost({
        ...zeroUsage,
        provider: 'grok',
        model: 'grok-4.5',
        input_tokens: 1_000_000,
        output_tokens: 1_000_000,
        cost_usd_ticks: 4 * 10_000_000_000, // $4 for the priced half
        uncosted_input_tokens: 1_000_000, // $2 at the table rate
        uncosted_output_tokens: 1_000_000, // $6 at the table rate
        uncosted_cache_read_tokens: 0,
        uncosted_cache_write_tokens: 0,
      }),
    ).toBeCloseTo(12, 5);
  });

  it('falls back to estimating the full row when the backend omits the split', () => {
    expect(
      estimateCost({
        ...zeroUsage,
        provider: 'grok',
        model: 'grok-4.5',
        input_tokens: 1_000_000,
        output_tokens: 1_000_000,
      }),
    ).toBeCloseTo(8, 5);
  });

  it('does not double-charge a cost that arrives without its token split', () => {
    expect(
      estimateCost({
        ...zeroUsage,
        provider: 'grok',
        model: 'grok-4.5',
        input_tokens: 1_000_000,
        output_tokens: 1_000_000,
        cost_usd_ticks: 16 * 10_000_000_000,
      }),
    ).toBeCloseTo(16, 5);
  });

  it('reports provider cost even for a model with no rate-table row', () => {
    expect(
      estimateCost({
        ...zeroUsage,
        provider: 'grok',
        model: 'grok-composer-2.5-fast',
        input_tokens: 500,
        output_tokens: 100,
        cost_usd_ticks: 12_345_678_900,
        uncosted_input_tokens: 0,
        uncosted_output_tokens: 0,
        uncosted_cache_read_tokens: 0,
        uncosted_cache_write_tokens: 0,
      }),
    ).toBeCloseTo(1.23456789, 8);
  });

  it('keeps the breakdown and the headline agreeing on an unpriced model', () => {
    const usage = {
      ...zeroUsage,
      provider: 'grok',
      model: 'grok-composer-2.5-fast',
      input_tokens: 500,
      output_tokens: 100,
      cost_usd_ticks: 12_345_678_900,
      uncosted_input_tokens: 0,
      uncosted_output_tokens: 0,
      uncosted_cache_read_tokens: 0,
      uncosted_cache_write_tokens: 0,
    };
    const b = estimateCostBreakdown(usage);
    expect(b.input + b.output + b.cacheRead + b.cacheWrite).toBeCloseTo(estimateCost(usage), 8);
    expect(b.input).toBeCloseTo(1.23456789, 8);
  });

  it('reports no cost for an unpriced model the provider did not price either', () => {
    const usage = {
      ...zeroUsage,
      provider: 'grok',
      model: 'grok-composer-2.5-fast',
      input_tokens: 500,
      output_tokens: 100,
    };
    expect(estimateCost(usage)).toBe(0);
    const b = estimateCostBreakdown(usage);
    expect(b.input + b.output + b.cacheRead + b.cacheWrite).toBe(0);
    expect(collectUnmappedModels([usage])).toEqual(['grok/grok-composer-2.5-fast']);
  });

  it('keeps the cost breakdown summing to the total on provider-priced rows', () => {
    const usage = {
      ...zeroUsage,
      provider: 'grok',
      model: 'grok-4.5',
      input_tokens: 1_000_000,
      output_tokens: 1_000_000,
      cost_usd_ticks: 16 * 10_000_000_000,
      uncosted_input_tokens: 0,
      uncosted_output_tokens: 0,
      uncosted_cache_read_tokens: 0,
      uncosted_cache_write_tokens: 0,
    };
    const b = estimateCostBreakdown(usage);
    expect(b.input + b.output + b.cacheRead + b.cacheWrite).toBeCloseTo(estimateCost(usage), 5);
    expect(b.input).toBeCloseTo(4, 5);
    expect(b.output).toBeCloseTo(12, 5);
  });

  it('drops a fully provider-priced model from the unmapped diagnostic', () => {
    const row = {
      ...zeroUsage,
      provider: 'grok',
      model: 'grok-composer-2.5-fast',
      input_tokens: 500,
      cost_usd_ticks: 12_345_678_900,
      uncosted_input_tokens: 0,
      uncosted_output_tokens: 0,
      uncosted_cache_read_tokens: 0,
      uncosted_cache_write_tokens: 0,
    };
    expect(collectUnmappedModels([row])).toEqual([]);
    expect(collectUnmappedModels([{ ...row, uncosted_input_tokens: 500 }])).toEqual([
      'grok/grok-composer-2.5-fast',
    ]);
  });

  it('leaves Grok SKUs that xAI does not publish a price for unmapped', () => {
    expect(isModelPriced('grok-composer-2.5-fast', 'xai')).toBe(false);
  });

  it('recognises the provider-prefixed forms emitted by OpenRouter-style runtimes', () => {
    expect(isModelPriced('deepseek/deepseek-v4-flash')).toBe(true);
    expect(isModelPriced('moonshotai/kimi-k2.6')).toBe(true);
    expect(isModelPriced('zhipuai/glm-5.1')).toBe(true);
    expect(isModelPriced('zhipuai/glm-4.5-air')).toBe(true);
  });
});

describe('isModelPriced', () => {
  it('recognises both Claude and Codex/GPT families', () => {
    expect(isModelPriced('claude-sonnet-5')).toBe(true);
    expect(isModelPriced('claude-fable-5')).toBe(true);
    expect(isModelPriced('claude-sonnet-4-6')).toBe(true);
    expect(isModelPriced('gpt-5-codex')).toBe(true);
    expect(isModelPriced('gpt-5-mini')).toBe(true);
    expect(isModelPriced('o3')).toBe(true);
    expect(isModelPriced('totally-made-up-model')).toBe(false);
  });

  it('recognises dotted Anthropic IDs as the same SKU as their dashed canonical form', () => {
    expect(isModelPriced('claude-sonnet-5')).toBe(true);
    expect(isModelPriced('claude-haiku-4.5')).toBe(true);
    expect(isModelPriced('claude-sonnet-4.5')).toBe(true);
    expect(isModelPriced('claude-sonnet-4.6')).toBe(true);
    expect(isModelPriced('claude-opus-4.5')).toBe(true);
    expect(isModelPriced('claude-opus-4.6')).toBe(true);
    expect(isModelPriced('claude-opus-4.7')).toBe(true);
  });

  it('recognises provider-prefixed Anthropic IDs (openclaw / opencode form)', () => {
    expect(isModelPriced('anthropic/claude-sonnet-5')).toBe(true);
    expect(isModelPriced('anthropic/claude-fable-5')).toBe(true);
    expect(isModelPriced('anthropic/claude-opus-4.7')).toBe(true);
    expect(isModelPriced('anthropic/claude-sonnet-4-6')).toBe(true);
  });

  it("still rejects OpenAI dotted variants that don't have their own row", () => {
    expect(isModelPriced('gpt-5.5-mini')).toBe(false);
  });
});

describe('collectUnmappedModels', () => {
  it('only surfaces names that miss every pricing tier', () => {
    const rows = [
      { ...zeroUsage, model: 'claude-sonnet-4-6' },
      { ...zeroUsage, model: 'gpt-5-codex' },
      { ...zeroUsage, model: 'fictional-model-x' },
    ];
    expect(collectUnmappedModels(rows)).toEqual(['fictional-model-x']);
  });
});

describe('user-supplied custom pricing', () => {
  it("prices a model the maintained catalog doesn't ship", () => {
    useCustomPricingStore.getState().setCustomPricing('gpt-5.5-mini', {
      input: 1,
      output: 4,
      cacheRead: 0.1,
      cacheWrite: 1,
    });
    expect(isModelPriced('gpt-5.5-mini')).toBe(true);
    expect(
      estimateCost({
        ...zeroUsage,
        model: 'gpt-5.5-mini',
        input_tokens: 1_000_000,
        output_tokens: 1_000_000,
      }),
    ).toBeCloseTo(5, 5);
  });

  it('does NOT shadow the maintained catalog when both define the same model', () => {
    useCustomPricingStore.getState().setCustomPricing('claude-sonnet-4-6', {
      input: 999,
      output: 999,
      cacheRead: 999,
      cacheWrite: 999,
    });
    expect(
      estimateCost({
        ...zeroUsage,
        model: 'claude-sonnet-4-6',
        input_tokens: 1_000_000,
      }),
    ).toBeCloseTo(3, 5); 
  });

  it('falls back to a stripped dated snapshot in the custom store', () => {
    useCustomPricingStore.getState().setCustomPricing('brand-new-model', {
      input: 2,
      output: 8,
      cacheRead: 0.2,
      cacheWrite: 2,
    });
    expect(
      estimateCost({
        ...zeroUsage,
        model: 'brand-new-model-2026-04-01',
        input_tokens: 1_000_000,
      }),
    ).toBeCloseTo(2, 5);
  });

  it('resolves a provider-qualified override only for the matching provider', () => {
    useCustomPricingStore.getState().setCustomPricing('acme/auto', {
      input: 2,
      output: 8,
      cacheRead: 0.2,
      cacheWrite: 2,
    });
    expect(
      estimateCost({ ...zeroUsage, provider: 'acme', model: 'auto', input_tokens: 1_000_000 }),
    ).toBeCloseTo(2, 5);
    expect(isModelPriced('auto')).toBe(false);
  });

  it('removeCustomPricing clears the override', () => {
    const store = useCustomPricingStore.getState();
    store.setCustomPricing('gpt-5.5-mini', {
      input: 1,
      output: 4,
      cacheRead: 0.1,
      cacheWrite: 1,
    });
    expect(isModelPriced('gpt-5.5-mini')).toBe(true);
    useCustomPricingStore.getState().removeCustomPricing('gpt-5.5-mini');
    expect(isModelPriced('gpt-5.5-mini')).toBe(false);
  });

  it('priced + unpriced models in the same window produce a mixed-cost aggregate', () => {
    const rows = [
      {
        ...zeroUsage,
        model: 'claude-sonnet-4-6',
        input_tokens: 1_000_000,
        date: '2026-01-01',
        provider: 'anthropic',
        agent_count: 1,
      },
      {
        ...zeroUsage,
        model: 'fictional-model-x',
        input_tokens: 1_000_000,
        date: '2026-01-01',
        provider: 'fictional',
        agent_count: 1,
      },
    ];
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const byModel = aggregateCostByModel(rows as any);
    const sonnet = byModel.find((r) => r.key === 'claude-sonnet-4-6');
    const fictional = byModel.find((r) => r.key === 'fictional/fictional-model-x');
    expect(sonnet?.cost).toBeCloseTo(3, 5);
    expect(fictional?.cost).toBe(0);
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    expect(collectUnmappedModels(rows as any)).toEqual(['fictional/fictional-model-x']);
  });

  it('keeps the same generic model id from two providers as distinct by-model rows', () => {
    const rows = [
      {
        ...zeroUsage,
        model: 'auto',
        provider: 'runtime-g',
        input_tokens: 1_000_000,
        date: '2026-01-01',
      },
      {
        ...zeroUsage,
        model: 'auto',
        provider: 'acme',
        input_tokens: 1_000_000,
        date: '2026-01-01',
      },
    ];
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const byModel = aggregateCostByModel(rows as any);
    expect(byModel.map((r) => r.key).toSorted()).toEqual(['acme/auto', 'runtime-g/auto']);
    expect(byModel.find((r) => r.key === 'runtime-g/auto')?.cost).toBeCloseTo(1.25, 5);
    expect(byModel.find((r) => r.key === 'acme/auto')?.cost).toBe(0);
  });

  it('aggregateCostByModel reflects a newly-saved custom price on re-call with the same input', () => {
    const rows = [
      {
        ...zeroUsage,
        model: 'fictional-model-x',
        input_tokens: 1_000_000,
        date: '2026-01-01',
        provider: 'fictional',
        agent_count: 1,
      },
    ];
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const before = aggregateCostByModel(rows as any);
    expect(before[0]?.cost).toBe(0);

    useCustomPricingStore.getState().setCustomPricing('fictional-model-x', {
      input: 2,
      output: 8,
      cacheRead: 0.2,
      cacheWrite: 2,
    });
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const after = aggregateCostByModel(rows as any);
    expect(after[0]?.cost).toBeCloseTo(2, 5);
  });
});

describe('weekStartIso', () => {
  it('returns the Monday of the same ISO week', () => {
    expect(weekStartIso('2026-05-19')).toBe('2026-05-18');
  });

  it('treats Monday as the start of its own week (idempotent)', () => {
    expect(weekStartIso('2026-05-18')).toBe('2026-05-18');
  });

  it('rolls Sunday back to the previous Monday', () => {
    expect(weekStartIso('2026-05-17')).toBe('2026-05-11');
  });

  it('crosses month and year boundaries', () => {
    expect(weekStartIso('2026-01-03')).toBe('2025-12-29');
  });
});

describe('addDaysIso', () => {
  it('adds across month boundary', () => {
    expect(addDaysIso('2026-05-30', 3)).toBe('2026-06-02');
  });

  it('subtracts across year boundary', () => {
    expect(addDaysIso('2026-01-02', -5)).toBe('2025-12-28');
  });
});

describe('todayIso', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("uses the runtime's timezone, not the host's, to decide today", () => {
    vi.setSystemTime(new Date('2026-05-19T16:00:00Z'));
    expect(todayIso('Asia/Shanghai')).toBe('2026-05-20');
    expect(todayIso('America/Los_Angeles')).toBe('2026-05-19');
    expect(todayIso('UTC')).toBe('2026-05-19');
  });
});

describe('sliceWindow (timezone-aware)', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  function makeUsage(date: string): RuntimeUsage {
    return {
      runtime_id: 'r',
      date,
      provider: 'anthropic',
      model: 'claude-sonnet-4-6',
      input_tokens: 0,
      output_tokens: 0,
      cache_read_tokens: 0,
      cache_write_tokens: 0,
    };
  }

  it('cuts the current window at today-in-tz, not today-in-host-utc', () => {
    vi.setSystemTime(new Date('2026-05-19T23:00:00Z'));
    const usage = [makeUsage('2026-05-13'), makeUsage('2026-05-19'), makeUsage('2026-05-20')];
    const { filtered } = sliceWindow(usage, 7, 'Asia/Shanghai');
    expect(filtered.map((u) => u.date)).toEqual(['2026-05-13', '2026-05-19', '2026-05-20']);
  });

  it('returns the immediately prior window of equal length', () => {
    vi.setSystemTime(new Date('2026-05-19T12:00:00Z'));
    const usage = [
      makeUsage('2026-05-01'),
      makeUsage('2026-05-08'),
      makeUsage('2026-05-15'),
      makeUsage('2026-05-19'),
    ];
    const { filtered, prevFiltered } = sliceWindow(usage, 7, 'UTC');
    expect(filtered.map((u) => u.date)).toEqual(['2026-05-15', '2026-05-19']);
    expect(prevFiltered.map((u) => u.date)).toEqual(['2026-05-08']);
  });
});

describe('aggregateByWeek', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  function makeUsage(date: string, input: number, output: number): RuntimeUsage {
    return {
      runtime_id: 'r',
      date,
      provider: 'anthropic',
      model: 'claude-sonnet-4-6',
      input_tokens: input,
      output_tokens: output,
      cache_read_tokens: 0,
      cache_write_tokens: 0,
    };
  }

  it('groups daily rows into Mon-anchored ISO weeks', () => {
    vi.setSystemTime(new Date('2026-05-24T12:00:00Z'));
    const rows = [
      makeUsage('2026-05-11', 1_000_000, 0),
      makeUsage('2026-05-17', 0, 1_000_000),
      makeUsage('2026-05-18', 2_000_000, 0),
    ];
    const { weeklyTokens } = aggregateByWeek(rows, 'UTC', 2);
    expect(weeklyTokens).toHaveLength(2);
    expect(weeklyTokens[0]).toMatchObject({
      weekStart: '2026-05-11',
      weekEnd: '2026-05-17',
      input: 1_000_000,
      output: 1_000_000,
      partial: false,
      daysCovered: 7,
    });
    expect(weeklyTokens[1]).toMatchObject({
      weekStart: '2026-05-18',
      weekEnd: '2026-05-24',
      input: 2_000_000,
      partial: false,
      daysCovered: 7,
    });
  });

  it('flags the in-progress week as partial with days-elapsed count', () => {
    vi.setSystemTime(new Date('2026-05-20T08:00:00Z'));
    const rows = [makeUsage('2026-05-18', 1_000_000, 0)];
    const { weeklyTokens } = aggregateByWeek(rows, 'UTC', 1);
    expect(weeklyTokens[0]).toMatchObject({
      weekStart: '2026-05-18',
      weekEnd: '2026-05-24',
      partial: true,
      daysCovered: 3, // Mon, Tue, Wed
    });
  });

  it('sums costs per week using the model pricing table', () => {
    vi.setSystemTime(new Date('2026-05-17T12:00:00Z'));
    const rows = [
      makeUsage('2026-05-11', 1_000_000, 1_000_000),
      makeUsage('2026-05-13', 1_000_000, 1_000_000),
    ];
    const { weeklyCostStack } = aggregateByWeek(rows, 'UTC', 1);
    expect(weeklyCostStack).toHaveLength(1);
    expect(weeklyCostStack[0]?.total).toBeCloseTo(36, 2);
  });

  it('emits trailing calendar weeks pinned to today, dropping older populated weeks', () => {
    vi.setSystemTime(new Date('2026-05-19T12:00:00Z'));
    const rows = [makeUsage('2026-04-13', 1_000_000, 1_000_000)];
    const { weeklyTokens, weeklyCostStack } = aggregateByWeek(rows, 'UTC', 5);

    expect(weeklyTokens.map((w) => w.weekStart)).toEqual([
      '2026-04-20',
      '2026-04-27',
      '2026-05-04',
      '2026-05-11',
      '2026-05-18',
    ]);
    for (const w of weeklyTokens) {
      expect(w.input).toBe(0);
      expect(w.output).toBe(0);
      expect(w.cacheRead).toBe(0);
      expect(w.cacheWrite).toBe(0);
    }
    for (const w of weeklyCostStack) {
      expect(w.total).toBe(0);
    }
  });

  it('keeps in-window weeks empty when nearby data sits inside the window', () => {
    vi.setSystemTime(new Date('2026-05-19T12:00:00Z'));
    const rows = [makeUsage('2026-04-22', 1_000_000, 1_000_000)]; 
    const { weeklyTokens } = aggregateByWeek(rows, 'UTC', 5);
    expect(weeklyTokens).toHaveLength(5);
    expect(weeklyTokens[0]).toMatchObject({
      weekStart: '2026-04-20',
      input: 1_000_000,
      output: 1_000_000,
    });
    for (const w of weeklyTokens.slice(1)) {
      expect(w.input).toBe(0);
      expect(w.output).toBe(0);
    }
  });
});

describe('computeCostInWindow', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  function priced(date: string, inputTokens: number): RuntimeUsage {
    return {
      runtime_id: 'r',
      date,
      provider: 'anthropic',
      model: 'claude-sonnet-4-6',
      input_tokens: inputTokens,
      output_tokens: 0,
      cache_read_tokens: 0,
      cache_write_tokens: 0,
    };
  }

  it('sums cost over the trailing daysBack window, end-exclusive of today', () => {
    vi.setSystemTime(new Date('2026-05-19T23:00:00Z'));
    const rows = [
      priced('2026-05-12', 1_000_000), // before window — excluded
      priced('2026-05-13', 1_000_000), // window start — included
      priced('2026-05-19', 1_000_000), // included
      priced('2026-05-20', 1_000_000), // today — excluded (end-exclusive)
    ];
    expect(computeCostInWindow(rows, 7, 'Asia/Shanghai')).toBeCloseTo(6, 5);
  });

  it('offsetDays shifts the window back to the prior period', () => {
    vi.setSystemTime(new Date('2026-05-20T12:00:00Z'));
    const rows = [
      priced('2026-05-05', 1_000_000), // before prior window — excluded
      priced('2026-05-06', 1_000_000), // prior window start — included
      priced('2026-05-12', 1_000_000), // included
      priced('2026-05-13', 1_000_000), // in the current window, not prior — excluded
    ];
    expect(computeCostInWindow(rows, 7, 'UTC', 7)).toBeCloseTo(6, 5);
  });

  it("reads 'today' in the supplied tz, not the host clock", () => {
    vi.setSystemTime(new Date('2026-05-19T20:00:00Z'));
    const rows = [priced('2026-05-19', 1_000_000)];
    expect(computeCostInWindow(rows, 1, 'UTC')).toBe(0); 
    expect(computeCostInWindow(rows, 1, 'Asia/Shanghai')).toBeCloseTo(3, 5);
  });

  it('returns 0 for an unpriced model rather than NaN', () => {
    vi.setSystemTime(new Date('2026-05-20T12:00:00Z'));
    const rows: RuntimeUsage[] = [
      { ...priced('2026-05-19', 1_000_000), model: 'totally-made-up-model' },
    ];
    expect(computeCostInWindow(rows, 7, 'UTC')).toBe(0);
  });

  it('returns 0 for an empty row set', () => {
    vi.setSystemTime(new Date('2026-05-20T12:00:00Z'));
    expect(computeCostInWindow([], 7, 'UTC')).toBe(0);
  });
});
