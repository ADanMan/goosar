import { describe, expect, it } from 'vitest';
import { RUNTIME_N_GATEWAY_TOKEN_MASK, serializeRuntimeNConfig } from './runtime-n-config';

describe('serializeRuntimeNConfig', () => {
  it('keeps the masked gateway token sentinel so the API can preserve the persisted token', () => {
    expect(
      serializeRuntimeNConfig({
        mode: 'gateway',
        gateway: {
          host: 'gw.internal',
          port: 18789,
          token: RUNTIME_N_GATEWAY_TOKEN_MASK,
          tls: true,
        },
      }),
    ).toEqual({
      mode: 'gateway',
      gateway: {
        host: 'gw.internal',
        port: 18789,
        token: RUNTIME_N_GATEWAY_TOKEN_MASK,
        tls: true,
      },
    });
  });

  it('omits an empty gateway token so users can clear a persisted token', () => {
    expect(
      serializeRuntimeNConfig({
        mode: 'gateway',
        gateway: {
          host: 'gw.internal',
          port: 18789,
        },
      }),
    ).toEqual({
      mode: 'gateway',
      gateway: {
        host: 'gw.internal',
        port: 18789,
      },
    });
  });

  it('passes through a real gateway token value', () => {
    expect(
      serializeRuntimeNConfig({
        mode: 'gateway',
        gateway: {
          token: 'rotated-secret',
        },
      }),
    ).toEqual({
      mode: 'gateway',
      gateway: {
        token: 'rotated-secret',
      },
    });
  });
});
