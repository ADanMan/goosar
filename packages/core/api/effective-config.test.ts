import { describe, expect, test } from 'vitest';
import { parseWithFallback } from './schema';
import {
  EMPTY_EFFECTIVE_CONFIG_VIEW,
  EffectiveConfigViewSchema,
  type EffectiveConfigView,
} from './effective-config';

const ENDPOINT = { endpoint: 'GET /api/effective-config' };

function parse(data: unknown): EffectiveConfigView {
  return parseWithFallback(data, EffectiveConfigViewSchema, EMPTY_EFFECTIVE_CONFIG_VIEW, ENDPOINT);
}

describe('EffectiveConfigViewSchema', () => {
  test('parses a full masked view', () => {
    const parsed = parse({
      schema_version: 1,
      llm: {
        base_url: 'https://gw.corp.example/v1',
        model: 'openai/coding-medium',
        has_api_key: true,
        origin: 'workspace',
        locked: false,
      },
      mcp: {
        outlook: { enabled: true, origin: 'policy', locked: true },
      },
      revoked_packages: [],
    });

    expect(parsed.llm?.origin).toBe('workspace');
    expect(parsed.llm?.has_api_key).toBe(true);
    expect(parsed.mcp?.outlook?.locked).toBe(true);
  });

  test('parses the empty-layers view (no llm, no mcp)', () => {
    const parsed = parse({ schema_version: 1, revoked_packages: [] });
    expect(parsed.llm).toBeUndefined();
    expect(parsed.mcp).toBeUndefined();
  });

  test('keeps an origin this build has never heard of', () => {
    const parsed = parse({
      schema_version: 2,
      llm: { has_api_key: false, origin: 'regional_policy', locked: true },
    });
    expect(parsed.llm?.origin).toBe('regional_policy');
    expect(parsed.llm?.locked).toBe(true);
  });

  test('defaults omitted booleans instead of failing', () => {
    const parsed = parse({ llm: { base_url: 'https://x.example/v1' } });
    expect(parsed.llm?.has_api_key).toBe(false);
    expect(parsed.llm?.locked).toBe(false);
    expect(parsed.llm?.origin).toBe('');
  });

  test('falls back to the empty view on a malformed response', () => {
    const parsed = parse({ llm: 'not-an-object' });
    expect(parsed).toEqual(EMPTY_EFFECTIVE_CONFIG_VIEW);
    expect(parsed.llm).toBeUndefined();
  });

  test('falls back to the empty view on a non-object response', () => {
    expect(parse(null)).toEqual(EMPTY_EFFECTIVE_CONFIG_VIEW);
    expect(parse('nope')).toEqual(EMPTY_EFFECTIVE_CONFIG_VIEW);
  });
});
