import { beforeEach, describe, expect, it } from 'vitest';
import {
  anySkillSourceEnabled,
  configStore,
  isPerimeterDeliveryProfile,
  isProviderAllowedByPolicy,
  isSkillSourceEnabled,
} from './index';

describe('configStore delivery profile', () => {
  beforeEach(() => {
    configStore.getState().setDeliveryProfile();
  });

  it('defaults to an empty profile (cloud behavior)', () => {
    expect(configStore.getState().deliveryProfile).toBe('');
  });

  it('stores a server-advertised perimeter profile', () => {
    configStore.getState().setDeliveryProfile('perimeter');
    expect(configStore.getState().deliveryProfile).toBe('perimeter');
  });

  it('resets to empty when the server omits the field', () => {
    configStore.getState().setDeliveryProfile('perimeter');
    configStore.getState().setDeliveryProfile(undefined);
    expect(configStore.getState().deliveryProfile).toBe('');
  });
});

describe('configStore allowed providers (issue #69)', () => {
  beforeEach(() => {
    configStore.getState().setAllowedProviders();
  });

  it('defaults to an empty list (unrestricted — cloud behavior)', () => {
    expect(configStore.getState().allowedProviders).toEqual([]);
  });

  it('stores the server-advertised policy list as a fresh copy', () => {
    const fromServer = ['runtime-j', 'runtime-c'];
    configStore.getState().setAllowedProviders(fromServer);
    expect(configStore.getState().allowedProviders).toEqual(['runtime-j', 'runtime-c']);
    expect(configStore.getState().allowedProviders).not.toBe(fromServer);
  });

  it('resets to unrestricted when the server omits the field', () => {
    configStore.getState().setAllowedProviders(['runtime-j']);
    configStore.getState().setAllowedProviders(undefined);
    expect(configStore.getState().allowedProviders).toEqual([]);
  });
});

describe('isProviderAllowedByPolicy', () => {
  it('allows everything when the list is absent or empty (older servers, managed cloud)', () => {
    expect(isProviderAllowedByPolicy(undefined, 'claude')).toBe(true);
    expect(isProviderAllowedByPolicy([], 'claude')).toBe(true);
  });

  it('allows exactly the listed providers, case/space-insensitively', () => {
    const policy = ['runtime-j', 'runtime-c'];
    expect(isProviderAllowedByPolicy(policy, 'runtime-j')).toBe(true);
    expect(isProviderAllowedByPolicy(policy, ' Runtime-C ')).toBe(true);
    expect(isProviderAllowedByPolicy(policy, 'runtime-e')).toBe(false);
  });
});

describe('isPerimeterDeliveryProfile', () => {
  it('is true only for the exact known perimeter value', () => {
    expect(isPerimeterDeliveryProfile('perimeter')).toBe(true);
  });

  it('falls back to cloud behavior for absent, empty, and unknown values', () => {
    expect(isPerimeterDeliveryProfile(undefined)).toBe(false);
    expect(isPerimeterDeliveryProfile('')).toBe(false);
    expect(isPerimeterDeliveryProfile('cloud')).toBe(false);
    expect(isPerimeterDeliveryProfile('airgap')).toBe(false);
  });

  it('accepts a store-shaped object via either field spelling', () => {
    expect(isPerimeterDeliveryProfile({ deliveryProfile: 'perimeter' })).toBe(true);
    expect(isPerimeterDeliveryProfile({ delivery_profile: 'perimeter' })).toBe(true);
    expect(isPerimeterDeliveryProfile({ deliveryProfile: 'cloud' })).toBe(false);
    expect(isPerimeterDeliveryProfile({})).toBe(false);
    expect(isPerimeterDeliveryProfile(null)).toBe(false);
    expect(isPerimeterDeliveryProfile(42)).toBe(false);
  });
});

describe('configStore skill sources (issue #66)', () => {
  beforeEach(() => {
    configStore.getState().setSkillSources();
  });

  it("defaults to null (every source enabled — today's behavior)", () => {
    expect(configStore.getState().skillSources).toBeNull();
  });

  it('stores a server-advertised list, including the empty perimeter default', () => {
    configStore.getState().setSkillSources(['github']);
    expect(configStore.getState().skillSources).toEqual(['github']);
    configStore.getState().setSkillSources([]);
    expect(configStore.getState().skillSources).toEqual([]);
  });

  it('resets to null when the server omits the field', () => {
    configStore.getState().setSkillSources(['clawhub']);
    configStore.getState().setSkillSources(undefined);
    expect(configStore.getState().skillSources).toBeNull();
  });
});

describe('isSkillSourceEnabled / anySkillSourceEnabled', () => {
  it('treats absent config as all-enabled (older servers, managed cloud)', () => {
    expect(isSkillSourceEnabled(null, 'clawhub')).toBe(true);
    expect(isSkillSourceEnabled(undefined, 'github')).toBe(true);
    expect(anySkillSourceEnabled(null)).toBe(true);
  });

  it('treats a present array as authoritative, including empty', () => {
    expect(isSkillSourceEnabled(['github'], 'github')).toBe(true);
    expect(isSkillSourceEnabled(['github'], 'clawhub')).toBe(false);
    expect(isSkillSourceEnabled([], 'clawhub')).toBe(false);
    expect(isSkillSourceEnabled([], 'github')).toBe(false);
    expect(isSkillSourceEnabled([], 'skillssh')).toBe(false);
    expect(anySkillSourceEnabled([])).toBe(false);
    expect(anySkillSourceEnabled(['skillssh'])).toBe(true);
  });

  it('ignores unknown extra source names from newer servers', () => {
    expect(isSkillSourceEnabled(['beehub'], 'clawhub')).toBe(false);
    expect(anySkillSourceEnabled(['beehub'])).toBe(false);
  });
});

describe('configStore MCP preset overlay', () => {
  beforeEach(() => {
    configStore.getState().setMcpPresetOverlay(null);
  });

  it('defaults to null (placeholders stand)', () => {
    expect(configStore.getState().mcpPresetOverlay).toBeNull();
  });

  it('stores a plain overlay object', () => {
    const overlay = {
      mcpServers: { atlassian: { env: { JIRA_URL: 'https://x' } } },
    };
    configStore.getState().setMcpPresetOverlay(overlay);
    expect(configStore.getState().mcpPresetOverlay).toEqual(overlay);
  });

  it('normalizes a malformed overlay (undefined / array) to null', () => {
    configStore.getState().setMcpPresetOverlay({ mcpServers: {} });
    configStore.getState().setMcpPresetOverlay(undefined);
    expect(configStore.getState().mcpPresetOverlay).toBeNull();

    configStore.getState().setMcpPresetOverlay([] as unknown as Record<string, unknown>);
    expect(configStore.getState().mcpPresetOverlay).toBeNull();
  });
});
