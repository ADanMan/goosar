import { describe, expect, it } from 'vitest';

import { providerSupportsMcpConfig } from './mcp-support';

describe('providerSupportsMcpConfig', () => {
  it('matches runtime codes whose runtime consumes mcp_config', () => {
    expect(providerSupportsMcpConfig('runtime-c')).toBe(true);
    expect(providerSupportsMcpConfig('runtime-d')).toBe(true);
    expect(providerSupportsMcpConfig('runtime-e')).toBe(true);
    expect(providerSupportsMcpConfig('runtime-g')).toBe(true);
    expect(providerSupportsMcpConfig('runtime-j')).toBe(true);
    expect(providerSupportsMcpConfig('runtime-k')).toBe(true);
    expect(providerSupportsMcpConfig('runtime-l')).toBe(true);
    expect(providerSupportsMcpConfig('runtime-m')).toBe(true);
    expect(providerSupportsMcpConfig('runtime-n')).toBe(true);
    expect(providerSupportsMcpConfig('runtime-p')).toBe(true);
    expect(providerSupportsMcpConfig('runtime-q')).toBe(true);
    expect(providerSupportsMcpConfig('runtime-r')).toBe(true);
    expect(providerSupportsMcpConfig('runtime-i')).toBe(true);
  });

  it('rejects runtime codes whose runtime ignores mcp_config', () => {
    expect(providerSupportsMcpConfig('runtime-a')).toBe(false);
    expect(providerSupportsMcpConfig('runtime-f')).toBe(false);
    expect(providerSupportsMcpConfig('runtime-o')).toBe(false);
    expect(providerSupportsMcpConfig(undefined)).toBe(false);
    expect(providerSupportsMcpConfig(null)).toBe(false);
  });
});
