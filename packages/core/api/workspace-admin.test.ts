import { describe, expect, it, test } from 'vitest';
import { parseWithFallback } from './schema';
import {
  EMPTY_PROVISIONING_CATALOG_VIEW,
  EMPTY_PROVISIONING_PINS_VIEW,
  EMPTY_USER_CONFIG_OVERRIDE_VIEW,
  EMPTY_WORKSPACE_CONFIG_VIEW,
  ProvisioningCatalogViewSchema,
  ProvisioningPinsViewSchema,
  UserConfigOverrideViewSchema,
  WorkspaceConfigViewSchema,
  type ProvisioningCatalogView,
  type ProvisioningPinsView,
  type UserConfigOverrideView,
  type WorkspaceConfigView,
} from './workspace-admin';

function parseWorkspaceConfig(data: unknown): WorkspaceConfigView {
  return parseWithFallback(data, WorkspaceConfigViewSchema, EMPTY_WORKSPACE_CONFIG_VIEW, {
    endpoint: 'GET /api/workspace-config',
  });
}

describe('WorkspaceConfigViewSchema', () => {
  test('parses a full masked workspace layer', () => {
    const parsed = parseWorkspaceConfig({
      llm_base_url: 'https://gw.corp.example/v1',
      llm_model: 'openai/coding-medium',
      has_llm_api_key: true,
      mcp_defaults: {
        outlook: { enabled: true, env: { OUTLOOK_TOKEN: true } },
        jira: { enabled: false },
      },
      updated_at: '2026-08-01T12:00:00Z',
      updated_by: '6ba7b810-9dad-11d1-80b4-00c04fd430c8',
    });

    expect(parsed.llm_base_url).toBe('https://gw.corp.example/v1');
    expect(parsed.has_llm_api_key).toBe(true);
    expect(parsed.mcp_defaults?.outlook?.enabled).toBe(true);
    expect(parsed.mcp_defaults?.outlook?.env?.OUTLOOK_TOKEN).toBe(true);
    expect(parsed.mcp_defaults?.jira?.enabled).toBe(false);
  });

  test("coerces a drifted backend's string env values to has_value markers", () => {
    const parsed = parseWorkspaceConfig({
      has_llm_api_key: false,
      mcp_defaults: {
        outlook: { enabled: true, env: { OUTLOOK_TOKEN: 'legacy-secret', EMPTY: '' } },
      },
    });
    expect(parsed.mcp_defaults?.outlook?.env).toEqual({
      OUTLOOK_TOKEN: true,
      EMPTY: false,
    });
  });

  test('parses the not-configured-yet empty object', () => {
    const parsed = parseWorkspaceConfig({ has_llm_api_key: false });
    expect(parsed.llm_base_url).toBeUndefined();
    expect(parsed.has_llm_api_key).toBe(false);
    expect(parsed.mcp_defaults).toBeUndefined();
  });

  test('defaults an omitted has_llm_api_key to false, never true', () => {
    const parsed = parseWorkspaceConfig({ llm_model: 'm' });
    expect(parsed.has_llm_api_key).toBe(false);
  });

  test('falls back on a malformed response (array body)', () => {
    const parsed = parseWorkspaceConfig(['not', 'an', 'object']);
    expect(parsed).toEqual(EMPTY_WORKSPACE_CONFIG_VIEW);
  });

  test('falls back when mcp_defaults is not an object', () => {
    const parsed = parseWorkspaceConfig({
      has_llm_api_key: true,
      mcp_defaults: 'oops',
    });
    expect(parsed).toEqual(EMPTY_WORKSPACE_CONFIG_VIEW);
  });

  test('tolerates unknown extra fields from a newer backend', () => {
    const parsed = parseWorkspaceConfig({
      has_llm_api_key: false,
      llm_region: 'eu-west-1',
    });
    expect(parsed.has_llm_api_key).toBe(false);
  });
});

function parseOverride(data: unknown): UserConfigOverrideView {
  return parseWithFallback(data, UserConfigOverrideViewSchema, EMPTY_USER_CONFIG_OVERRIDE_VIEW, {
    endpoint: 'GET /api/workspace-config/overrides/{userId}',
  });
}

describe('UserConfigOverrideViewSchema', () => {
  test('parses a full override row', () => {
    const parsed = parseOverride({
      user_id: '6ba7b810-9dad-11d1-80b4-00c04fd430c8',
      llm_base_url: 'https://personal.example/v1',
      has_llm_api_key: true,
      mcp_overrides: { github: { enabled: true } },
    });
    expect(parsed.user_id).toBe('6ba7b810-9dad-11d1-80b4-00c04fd430c8');
    expect(parsed.has_llm_api_key).toBe(true);
    expect(parsed.mcp_overrides?.github?.enabled).toBe(true);
  });

  test('parses a key-only override (no llm coordinates)', () => {
    const parsed = parseOverride({ user_id: 'u-1', has_llm_api_key: true });
    expect(parsed.llm_base_url).toBeUndefined();
    expect(parsed.has_llm_api_key).toBe(true);
  });

  test('falls back on a malformed response', () => {
    const parsed = parseOverride(42);
    expect(parsed).toEqual(EMPTY_USER_CONFIG_OVERRIDE_VIEW);
  });
});

function parsePins(data: unknown): ProvisioningPinsView {
  return parseWithFallback(data, ProvisioningPinsViewSchema, EMPTY_PROVISIONING_PINS_VIEW, {
    endpoint: 'GET /api/provisioning/pins',
  });
}

describe('ProvisioningPinsViewSchema', () => {
  test('parses the pins envelope', () => {
    const parsed = parsePins({
      pins: [
        {
          package_name: 'docx-skill',
          package_type: 'skill',
          version: '1.2.0',
          enabled: true,
          updated_at: '2026-08-01T12:00:00Z',
          updated_by: '6ba7b810-9dad-11d1-80b4-00c04fd430c8',
        },
      ],
    });
    expect(parsed.pins).toHaveLength(1);
    expect(parsed.pins[0]?.package_name).toBe('docx-skill');
    expect(parsed.pins[0]?.enabled).toBe(true);
  });

  test('parses the no-pins state (empty list = whole catalog, #187)', () => {
    const parsed = parsePins({ pins: [] });
    expect(parsed.pins).toEqual([]);
  });

  test('defaults an omitted enabled to false, never true', () => {
    const parsed = parsePins({
      pins: [{ package_name: 'p', package_type: 'skill', version: '1' }],
    });
    expect(parsed.pins[0]?.enabled).toBe(false);
  });

  test('falls back on a malformed response (bare array, no envelope)', () => {
    const parsed = parsePins([{ package_name: 'p' }]);
    expect(parsed).toEqual(EMPTY_PROVISIONING_PINS_VIEW);
  });
});

function parseCatalog(data: unknown): ProvisioningCatalogView {
  return parseWithFallback(data, ProvisioningCatalogViewSchema, EMPTY_PROVISIONING_CATALOG_VIEW, {
    endpoint: 'GET /api/provisioning/catalog',
  });
}

describe('ProvisioningCatalogViewSchema', () => {
  test('parses a published catalog', () => {
    const parsed = parseCatalog({
      schemaVersion: 1,
      packages: [
        {
          schemaVersion: 1,
          name: 'docx-skill',
          version: '1.2.0',
          type: 'skill',
          platform: 'any',
          sha256: 'ab'.repeat(32),
          size: 1024,
          requires: ['runtime:python@3.12'],
        },
      ],
    });
    expect(parsed.packages).toHaveLength(1);
    expect(parsed.packages[0]?.name).toBe('docx-skill');
    expect(parsed.packages[0]?.type).toBe('skill');
  });

  test('parses an empty catalog (nothing published yet)', () => {
    const parsed = parseCatalog({ schemaVersion: 1, packages: [] });
    expect(parsed.packages).toEqual([]);
  });

  test('keeps a package type this build has never heard of', () => {
    const parsed = parseCatalog({
      packages: [{ name: 'x', version: '1', type: 'prompt-pack' }],
    });
    expect(parsed.packages[0]?.type).toBe('prompt-pack');
  });

  test('falls back on a malformed response', () => {
    const parsed = parseCatalog({ packages: 'nope' });
    expect(parsed).toEqual(EMPTY_PROVISIONING_CATALOG_VIEW);
  });
});

describe('admin cache hygiene', () => {
  it('declares zero gcTime on every secret-bearing query and mutation', async () => {
    const mod = await import('../workspace/admin-config');
    expect(mod.workspaceConfigOptions('ws-1').gcTime).toBe(0);
    expect(mod.userConfigOverrideOptions('ws-1', 'u-1').gcTime).toBe(0);

    const source = (await import('node:fs')).readFileSync(
      new URL('../workspace/admin-config.ts', import.meta.url),
      'utf8',
    );
    const mutations = source.match(/useMutation\(\{/g)?.length ?? 0;
    const zeroGc = source.match(/gcTime: 0,/g)?.length ?? 0;
    expect(mutations).toBeGreaterThan(0);
    expect(zeroGc).toBe(2 + mutations);
  });
});
