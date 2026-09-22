import { describe, expect, it } from 'vitest';
import {
  applyRuntimeConfigPatch,
  DEFAULT_RUNTIME_CONFIG,
  deriveAppUrl,
  deriveWsUrl,
  endpointPinsReplacedBy,
  normalizeApiUrl,
  parseRuntimeConfig,
  parseRuntimeConfigPatch,
  runtimeConfigFromDevEnv,
} from './runtime-config';

describe('runtime config', () => {
  it('ships a default that obeys the runtime config schema', () => {
    expect(DEFAULT_RUNTIME_CONFIG.schemaVersion).toBe(1);
    expect(new URL(DEFAULT_RUNTIME_CONFIG.apiUrl).protocol).toBe('https:');
    expect(DEFAULT_RUNTIME_CONFIG.apiUrl).not.toMatch(/\/$/);
    expect(Object.isFrozen(DEFAULT_RUNTIME_CONFIG)).toBe(true);
  });

  it('ships a default whose ws/app endpoints follow the derivation rules', () => {
    expect(DEFAULT_RUNTIME_CONFIG.wsUrl).toBe(deriveWsUrl(DEFAULT_RUNTIME_CONFIG.apiUrl));
    expect(DEFAULT_RUNTIME_CONFIG.appUrl).toBe(deriveAppUrl(DEFAULT_RUNTIME_CONFIG.apiUrl));
  });

  it('round-trips the default through the config parser unchanged', () => {
    expect(
      parseRuntimeConfig(
        JSON.stringify({ schemaVersion: 1, apiUrl: DEFAULT_RUNTIME_CONFIG.apiUrl }),
      ),
    ).toEqual({ ...DEFAULT_RUNTIME_CONFIG });
  });

  it('derives https/wss compatible URLs from apiUrl', () => {
    expect(
      parseRuntimeConfig(
        JSON.stringify({
          schemaVersion: 1,
          apiUrl: 'https://congvc-x99.taila6fa8a.ts.net:18443',
        }),
      ),
    ).toEqual({
      schemaVersion: 1,
      apiUrl: 'https://congvc-x99.taila6fa8a.ts.net:18443',
      wsUrl: 'wss://congvc-x99.taila6fa8a.ts.net:18443/ws',
      appUrl: 'https://congvc-x99.taila6fa8a.ts.net:18443',
    });
  });

  it('strips the leading api. label when deriving appUrl', () => {
    expect(
      parseRuntimeConfig(JSON.stringify({ schemaVersion: 1, apiUrl: 'https://api.goosar.ru' })),
    ).toEqual({
      schemaVersion: 1,
      apiUrl: 'https://api.goosar.ru',
      wsUrl: 'wss://api.goosar.ru/ws',
      appUrl: 'https://goosar.ru',
    });
  });

  it('derives ws for http api URLs', () => {
    expect(deriveWsUrl('http://localhost:8080')).toBe('ws://localhost:8080/ws');
  });

  it('accepts explicit appUrl and wsUrl', () => {
    expect(
      parseRuntimeConfig(
        JSON.stringify({
          schemaVersion: 1,
          apiUrl: 'https://api.example.com/',
          wsUrl: 'wss://ws.example.com/socket/',
          appUrl: 'https://app.example.com/',
        }),
      ),
    ).toEqual({
      schemaVersion: 1,
      apiUrl: 'https://api.example.com',
      wsUrl: 'wss://ws.example.com/socket',
      appUrl: 'https://app.example.com',
    });
  });

  it('rejects invalid JSON', () => {
    expect(() => parseRuntimeConfig('{')).toThrow(/Invalid desktop runtime config JSON/);
  });

  it('rejects unsupported schema versions', () => {
    expect(() =>
      parseRuntimeConfig(JSON.stringify({ schemaVersion: 2, apiUrl: 'https://api.example.com' })),
    ).toThrow(/schemaVersion/);
  });

  it('rejects non-http api schemes', () => {
    expect(() =>
      parseRuntimeConfig(JSON.stringify({ schemaVersion: 1, apiUrl: 'file:///tmp/goosar' })),
    ).toThrow(/apiUrl must use http or https/);
  });

  it('rejects non-ws websocket schemes', () => {
    expect(() =>
      parseRuntimeConfig(
        JSON.stringify({
          schemaVersion: 1,
          apiUrl: 'https://api.example.com',
          wsUrl: 'https://api.example.com/ws',
        }),
      ),
    ).toThrow(/wsUrl must use ws or wss/);
  });

  it('preserves electron-vite dev env precedence', () => {
    expect(
      runtimeConfigFromDevEnv({
        apiUrl: 'http://dev-api.example.test:8080/',
        wsUrl: 'ws://dev-api.example.test:8080/ws/',
        appUrl: 'http://dev-app.example.test:3000/',
      }),
    ).toEqual({
      schemaVersion: 1,
      apiUrl: 'http://dev-api.example.test:8080',
      wsUrl: 'ws://dev-api.example.test:8080/ws',
      appUrl: 'http://dev-app.example.test:3000',
    });
  });

  it('falls back to local web URL when dev apiUrl is localhost', () => {
    expect(runtimeConfigFromDevEnv({ apiUrl: 'http://localhost:8080' })).toEqual({
      schemaVersion: 1,
      apiUrl: 'http://localhost:8080',
      wsUrl: 'ws://localhost:8080/ws',
      appUrl: 'http://localhost:3000',
    });
  });

  it('derives dev appUrl by stripping the leading api. label', () => {
    expect(runtimeConfigFromDevEnv({ apiUrl: 'https://api.test.goosar.ru' })).toEqual({
      schemaVersion: 1,
      apiUrl: 'https://api.test.goosar.ru',
      wsUrl: 'wss://api.test.goosar.ru/ws',
      appUrl: 'https://test.goosar.ru',
    });
  });

  it('dev VITE_APP_URL still wins over apiUrl-derived value', () => {
    expect(
      runtimeConfigFromDevEnv({
        apiUrl: 'https://api.test.goosar.ru',
        appUrl: 'https://staging.goosar.ru',
      }),
    ).toEqual({
      schemaVersion: 1,
      apiUrl: 'https://api.test.goosar.ru',
      wsUrl: 'wss://api.test.goosar.ru/ws',
      appUrl: 'https://staging.goosar.ru',
    });
  });
});

describe('normalizeApiUrl', () => {
  it('upgrades a plain http address to https', () => {
    expect(normalizeApiUrl('http://goosar.ru')).toBe('https://goosar.ru');
  });

  it('prepends https to a bare host with no scheme', () => {
    expect(normalizeApiUrl('goosar.ru')).toBe('https://goosar.ru');
  });

  it('lowercases the scheme and host and trims a trailing slash', () => {
    expect(normalizeApiUrl('HTTP://Goosar.RU/')).toBe('https://goosar.ru');
  });

  it('leaves an http localhost address unchanged', () => {
    expect(normalizeApiUrl('http://localhost:8080')).toBe('http://localhost:8080');
  });

  it('leaves an http 127.0.0.1 address unchanged', () => {
    expect(normalizeApiUrl('http://127.0.0.1:3000')).toBe('http://127.0.0.1:3000');
  });

  it('leaves an http 192.168.x.x address unchanged', () => {
    expect(normalizeApiUrl('http://192.168.1.5')).toBe('http://192.168.1.5');
  });

  it('leaves an https address unchanged', () => {
    expect(normalizeApiUrl('https://x.example')).toBe('https://x.example');
  });

  it('still rejects garbage input as an invalid URL', () => {
    expect(() => normalizeApiUrl('not a url')).toThrow(/apiUrl must be a valid URL/);
  });

  it('still rejects a non-http(s) scheme', () => {
    expect(() => normalizeApiUrl('file:///tmp/goosar')).toThrow(/apiUrl must use http or https/);
  });
});

describe('parseRuntimeConfigPatch', () => {
  it('accepts an apiUrl-only patch', () => {
    expect(parseRuntimeConfigPatch({ apiUrl: 'https://goosar.acme.test' })).toEqual({
      apiUrl: 'https://goosar.acme.test',
      wsUrl: undefined,
      appUrl: undefined,
    });
  });

  it('rejects a non-object payload', () => {
    expect(() => parseRuntimeConfigPatch('https://goosar.acme.test')).toThrow(/expected an object/);
    expect(() => parseRuntimeConfigPatch(null)).toThrow(/expected an object/);
    expect(() => parseRuntimeConfigPatch([])).toThrow(/expected an object/);
  });

  it('rejects a missing or blank apiUrl', () => {
    expect(() => parseRuntimeConfigPatch({})).toThrow(/apiUrl/);
    expect(() => parseRuntimeConfigPatch({ apiUrl: '   ' })).toThrow(/apiUrl/);
    expect(() => parseRuntimeConfigPatch({ apiUrl: 42 })).toThrow(/apiUrl/);
  });
});

describe('applyRuntimeConfigPatch', () => {
  it('builds a first-time document when no config file exists', () => {
    const result = applyRuntimeConfigPatch(null, {
      apiUrl: 'https://goosar.acme.test/',
    });

    expect(result.config).toEqual({
      schemaVersion: 1,
      apiUrl: 'https://goosar.acme.test',
      wsUrl: 'wss://goosar.acme.test/ws',
      appUrl: 'https://goosar.acme.test',
    });
    expect(JSON.parse(result.json)).toEqual({
      schemaVersion: 1,
      apiUrl: 'https://goosar.acme.test/',
    });
  });

  it('preserves top-level fields the schema does not model', () => {
    const existing = JSON.stringify({
      schemaVersion: 1,
      apiUrl: 'https://old.acme.test',
      telemetryOptOut: true,
      proxy: { host: 'proxy.acme.test', port: 3128 },
    });

    const result = applyRuntimeConfigPatch(existing, {
      apiUrl: 'https://new.acme.test',
    });

    expect(JSON.parse(result.json)).toEqual({
      schemaVersion: 1,
      apiUrl: 'https://new.acme.test',
      telemetryOptOut: true,
      proxy: { host: 'proxy.acme.test', port: 3128 },
    });
  });

  it('drops a stale explicit wsUrl/appUrl so they re-derive from the new apiUrl', () => {
    const existing = JSON.stringify({
      schemaVersion: 1,
      apiUrl: 'https://old.acme.test',
      wsUrl: 'wss://old.acme.test/ws',
      appUrl: 'https://app.old.acme.test',
    });

    const result = applyRuntimeConfigPatch(existing, {
      apiUrl: 'https://new.acme.test',
    });

    expect(JSON.parse(result.json)).toEqual({
      schemaVersion: 1,
      apiUrl: 'https://new.acme.test',
    });
    expect(result.config).toEqual({
      schemaVersion: 1,
      apiUrl: 'https://new.acme.test',
      wsUrl: 'wss://new.acme.test/ws',
      appUrl: 'https://new.acme.test',
    });
  });

  it('keeps wsUrl/appUrl that the patch supplies explicitly', () => {
    const result = applyRuntimeConfigPatch(null, {
      apiUrl: 'https://api.acme.test',
      wsUrl: 'wss://ws.acme.test/socket',
      appUrl: 'https://app.acme.test',
    });

    expect(result.config).toEqual({
      schemaVersion: 1,
      apiUrl: 'https://api.acme.test',
      wsUrl: 'wss://ws.acme.test/socket',
      appUrl: 'https://app.acme.test',
    });
  });

  it('replaces a corrupt document instead of preserving unreadable bytes', () => {
    const result = applyRuntimeConfigPatch('{ not json', {
      apiUrl: 'https://goosar.acme.test',
    });

    expect(JSON.parse(result.json)).toEqual({
      schemaVersion: 1,
      apiUrl: 'https://goosar.acme.test',
    });
  });

  it('rejects an apiUrl that is not http or https', () => {
    expect(() => applyRuntimeConfigPatch(null, { apiUrl: 'file:///tmp/goosar' })).toThrow(
      /apiUrl must use http or https/,
    );
  });

  it('rejects an apiUrl that is not a URL at all', () => {
    expect(() => applyRuntimeConfigPatch(null, { apiUrl: 'not a url' })).toThrow(
      /apiUrl must be a valid URL/,
    );
  });

  it('rejects a wsUrl that is not a websocket URL', () => {
    expect(() =>
      applyRuntimeConfigPatch(null, {
        apiUrl: 'https://api.acme.test',
        wsUrl: 'https://api.acme.test/ws',
      }),
    ).toThrow(/wsUrl must use ws or wss/);
  });
});

describe('endpointPinsReplacedBy', () => {
  function configFor(fields: Record<string, string>) {
    return parseRuntimeConfig(JSON.stringify({ schemaVersion: 1, ...fields }));
  }

  it('names a pinned wsUrl that changing the server would replace', () => {
    const current = configFor({
      apiUrl: 'https://api.old.acme.test',
      wsUrl: 'wss://sockets.acme.test/gateway',
    });

    expect(endpointPinsReplacedBy(current, 'https://api.new.acme.test')).toEqual([
      {
        field: 'wsUrl',
        previous: 'wss://sockets.acme.test/gateway',
        next: 'wss://api.new.acme.test/ws',
      },
    ]);
  });

  it('names a pinned appUrl that changing the server would replace', () => {
    const current = configFor({
      apiUrl: 'https://api.old.acme.test',
      appUrl: 'https://portal.acme.test',
    });

    expect(endpointPinsReplacedBy(current, 'https://api.new.acme.test')).toEqual([
      {
        field: 'appUrl',
        previous: 'https://portal.acme.test',
        next: 'https://new.acme.test',
      },
    ]);
  });

  it('names both pins when both are provisioned', () => {
    const current = configFor({
      apiUrl: 'https://api.old.acme.test',
      wsUrl: 'wss://sockets.acme.test/gateway',
      appUrl: 'https://portal.acme.test',
    });

    expect(
      endpointPinsReplacedBy(current, 'https://api.new.acme.test').map((p) => p.field),
    ).toEqual(['wsUrl', 'appUrl']);
  });

  it('stays quiet when the endpoints were derived rather than pinned', () => {
    const current = configFor({ apiUrl: 'https://api.old.acme.test' });

    expect(endpointPinsReplacedBy(current, 'https://api.new.acme.test')).toEqual([]);
  });

  it('stays quiet when a pin already holds the value the new server derives', () => {
    const current = configFor({
      apiUrl: 'https://api.old.acme.test',
      wsUrl: 'wss://api.new.acme.test/ws',
      appUrl: 'https://new.acme.test',
    });

    expect(endpointPinsReplacedBy(current, 'https://api.new.acme.test')).toEqual([]);
  });

  it("does not mistake a dev build's derived appUrl for an operator pin", () => {
    const current = runtimeConfigFromDevEnv({ apiUrl: 'http://localhost:8080' });

    expect(endpointPinsReplacedBy(current, 'https://api.new.acme.test')).toEqual([]);
  });

  it('warns even when the address is re-applied unchanged', () => {
    const current = configFor({
      apiUrl: 'https://api.acme.test',
      wsUrl: 'wss://sockets.acme.test/gateway',
    });

    expect(endpointPinsReplacedBy(current, 'https://api.acme.test')).toEqual([
      {
        field: 'wsUrl',
        previous: 'wss://sockets.acme.test/gateway',
        next: 'wss://api.acme.test/ws',
      },
    ]);
  });
});
