import { describe, expect, it } from 'vitest';
import { buildHelperMcpConfig, HELPER_MCP_PRESET_NAMES, WORK_TOOLS_CARD_ORDER } from './index';
import {
  isPresetInstalled,
  SERVICE_DISPLAY_ORDER,
  SERVICE_IDENTITIES,
  serviceIdentityByPackageName,
  serviceIdentityByPreset,
} from './service-identity';

describe('SERVICE_IDENTITIES covers the catalog', () => {
  it('has one entry per credential preset and no strays', () => {
    expect(SERVICE_IDENTITIES.map((s) => s.preset).sort()).toEqual(
      [...HELPER_MCP_PRESET_NAMES].sort(),
    );
  });

  it('lists services in the same order the credential cards use', () => {
    expect(SERVICE_DISPLAY_ORDER).toEqual(WORK_TOOLS_CARD_ORDER);
  });

  it('maps every package name back to exactly one preset', () => {
    const seen = new Set<string>();
    for (const identity of SERVICE_IDENTITIES) {
      for (const packageName of identity.packageNames) {
        expect(seen.has(packageName)).toBe(false);
        seen.add(packageName);
        expect(serviceIdentityByPackageName(packageName)?.preset).toBe(identity.preset);
      }
    }
  });

  it('names the corporate servers the workspace templates actually pin', () => {
    expect(serviceIdentityByPackageName('ews-mcp')?.preset).toBe('outlook');
    expect(serviceIdentityByPackageName('b24-agent')?.preset).toBe('bitrix24');
  });

  it('returns null for names it has never heard of, rather than guessing', () => {
    expect(serviceIdentityByPackageName('some-other-server')).toBeNull();
    expect(serviceIdentityByPreset('not-a-preset')).toBeNull();
  });
});

describe('personal credential slots match the catalog', () => {
  const pristine = buildHelperMcpConfig().mcpServers;

  it('names only env keys the preset actually declares', () => {
    for (const identity of SERVICE_IDENTITIES) {
      const env = pristine[identity.preset]?.env ?? {};
      for (const key of identity.personalEnvKeys) {
        expect(Object.keys(env)).toContain(key);
      }
    }
  });

  it("reads the gateway's personal value from env, not args (issue #704)", () => {
    expect(serviceIdentityByPreset('mcp-gateway')?.personalEnvKeys).toEqual(['API_ACCESS_TOKEN']);
  });

  it('points every unfilled-slot pattern at a key the preset declares', () => {
    for (const identity of SERVICE_IDENTITIES) {
      const env = pristine[identity.preset]?.env ?? {};
      for (const [key, pattern] of Object.entries(identity.unfilledEnvPatterns ?? {})) {
        expect(identity.personalEnvKeys).toContain(key);
        expect(pattern.test(env[key] ?? '')).toBe(true);
      }
    }
  });

  it('marks fetch as the one service with nothing for the member to enter', () => {
    expect(SERVICE_IDENTITIES.filter((s) => !s.hasPersonalCredential).map((s) => s.preset)).toEqual(
      ['fetch'],
    );
  });
});

describe('isPresetInstalled', () => {
  it('matches a corporate server by the package name provisioning reports', () => {
    expect(isPresetInstalled('outlook', ['ews-mcp'])).toBe(true);
    expect(isPresetInstalled('bitrix24', ['b24-agent'])).toBe(true);
  });

  it('still matches by preset name', () => {
    expect(isPresetInstalled('atlassian', ['atlassian'])).toBe(true);
    expect(isPresetInstalled('outlook', ['outlook'])).toBe(true);
  });

  it('does not match a service the list never mentions', () => {
    expect(isPresetInstalled('outlook', ['b24-agent'])).toBe(false);
    expect(isPresetInstalled('atlassian', [])).toBe(false);
  });
});
