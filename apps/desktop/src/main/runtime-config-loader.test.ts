import { mkdtemp, readFile, stat, writeFile } from 'fs/promises';
import { join } from 'path';
import { tmpdir } from 'os';
import { describe, expect, it, vi } from 'vitest';
import { DEFAULT_RUNTIME_CONFIG } from '../shared/runtime-config';
import {
  loadRuntimeConfig,
  probeRuntimeServer,
  saveDebugLoggingToggle,
  saveRuntimeConfig,
} from './runtime-config-loader';

describe('loadRuntimeConfig', () => {
  it('uses dev env and ignores desktop.json during electron-vite dev', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    const configPath = join(dir, 'desktop.json');
    await writeFile(
      configPath,
      JSON.stringify({ schemaVersion: 1, apiUrl: 'https://prod.example.com' }),
    );

    await expect(
      loadRuntimeConfig({
        isDev: true,
        configPath,
        env: {
          apiUrl: 'http://localhost:8080',
          wsUrl: 'ws://localhost:8080/ws',
          appUrl: 'http://localhost:3000',
        },
      }),
    ).resolves.toEqual({
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: 'http://localhost:8080',
        wsUrl: 'ws://localhost:8080/ws',
        appUrl: 'http://localhost:3000',
      },
    });
  });

  it('uses the shipped default when packaged config is absent', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    await expect(
      loadRuntimeConfig({
        isDev: false,
        configPath: join(dir, 'missing.json'),
        env: {},
      }),
    ).resolves.toEqual({ ok: true, config: { ...DEFAULT_RUNTIME_CONFIG } });
  });

  it('parses a valid packaged desktop.json', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    const configPath = join(dir, 'desktop.json');
    await writeFile(
      configPath,
      JSON.stringify({ schemaVersion: 1, apiUrl: 'https://api.example.com' }),
    );

    await expect(loadRuntimeConfig({ isDev: false, configPath, env: {} })).resolves.toEqual({
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: 'https://api.example.com',
        wsUrl: 'wss://api.example.com/ws',
        appUrl: 'https://example.com',
      },
    });
  });

  it('upgrades a stored http apiUrl for a non-loopback host to https on load', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    const configPath = join(dir, 'desktop.json');
    await writeFile(configPath, JSON.stringify({ schemaVersion: 1, apiUrl: 'http://goosar.ru' }));

    await expect(loadRuntimeConfig({ isDev: false, configPath, env: {} })).resolves.toEqual({
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: 'https://goosar.ru',
        wsUrl: 'wss://goosar.ru/ws',
        appUrl: 'https://goosar.ru',
      },
    });
  });

  it('leaves a stored http apiUrl on a loopback host unchanged on load', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    const configPath = join(dir, 'desktop.json');
    await writeFile(
      configPath,
      JSON.stringify({ schemaVersion: 1, apiUrl: 'http://127.0.0.1:8080' }),
    );

    const result = await loadRuntimeConfig({ isDev: false, configPath, env: {} });

    expect(result).toEqual({
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: 'http://127.0.0.1:8080',
        wsUrl: 'ws://127.0.0.1:8080/ws',
        appUrl: 'http://127.0.0.1:8080',
      },
    });
  });

  it('fails closed when packaged desktop.json is invalid', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    const configPath = join(dir, 'desktop.json');
    await writeFile(configPath, '{');

    const result = await loadRuntimeConfig({ isDev: false, configPath, env: {} });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.message).toContain(configPath);
      expect(result.error.message).toContain('Invalid desktop runtime config JSON');
    }
  });
});

describe('saveRuntimeConfig', () => {
  it('persists a new server address and returns the effective config', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    const configPath = join(dir, 'desktop.json');

    const result = await saveRuntimeConfig({
      patch: { apiUrl: 'https://goosar.acme.test' },
      configPath,
    });

    expect(result).toEqual({
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: 'https://goosar.acme.test',
        wsUrl: 'wss://goosar.acme.test/ws',
        appUrl: 'https://goosar.acme.test',
      },
    });
  });

  it('is what the next load returns', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    const configPath = join(dir, 'desktop.json');

    await saveRuntimeConfig({
      patch: { apiUrl: 'https://goosar.acme.test' },
      configPath,
    });

    await expect(loadRuntimeConfig({ isDev: false, configPath, env: {} })).resolves.toEqual({
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: 'https://goosar.acme.test',
        wsUrl: 'wss://goosar.acme.test/ws',
        appUrl: 'https://goosar.acme.test',
      },
    });
  });

  it('creates the config directory when it does not exist yet', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    const configPath = join(dir, 'nested', '.goosar', 'desktop.json');

    const result = await saveRuntimeConfig({
      patch: { apiUrl: 'https://goosar.acme.test' },
      configPath,
    });

    expect(result.ok).toBe(true);
    await expect(readFile(configPath, 'utf-8')).resolves.toContain('https://goosar.acme.test');
  });

  it('merges over operator-provisioned fields the schema does not model', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    const configPath = join(dir, 'desktop.json');
    await writeFile(
      configPath,
      JSON.stringify({
        schemaVersion: 1,
        apiUrl: 'https://old.acme.test',
        proxy: { host: 'proxy.acme.test', port: 3128 },
      }),
    );

    await saveRuntimeConfig({
      patch: { apiUrl: 'https://new.acme.test' },
      configPath,
    });

    const persisted = JSON.parse(await readFile(configPath, 'utf-8'));
    expect(persisted).toEqual({
      schemaVersion: 1,
      apiUrl: 'https://new.acme.test',
      proxy: { host: 'proxy.acme.test', port: 3128 },
    });
  });

  it('rejects garbage without touching the existing file', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    const configPath = join(dir, 'desktop.json');
    const original = JSON.stringify({
      schemaVersion: 1,
      apiUrl: 'https://good.acme.test',
    });
    await writeFile(configPath, original);

    const result = await saveRuntimeConfig({
      patch: { apiUrl: 'javascript:alert(1)' },
      configPath,
    });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.message).toMatch(/apiUrl must use http or https/);
    }
    await expect(readFile(configPath, 'utf-8')).resolves.toBe(original);
  });

  it('rejects a patch that is not an object', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    const configPath = join(dir, 'desktop.json');

    const result = await saveRuntimeConfig({ patch: 'nope', configPath });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.message).toMatch(/expected an object/);
    }
  });

  it('writes desktop.json at mode 0600, not group/world-readable', async () => {
    if (process.platform === 'win32') return; 
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    const configPath = join(dir, 'desktop.json');

    await saveRuntimeConfig({
      patch: { apiUrl: 'https://goosar.acme.test' },
      configPath,
    });

    const mode = (await stat(configPath)).mode & 0o777;
    expect(mode).toBe(0o600);
  });
});

describe('saveDebugLoggingToggle', () => {
  it('creates a boot-valid desktop.json when none exists', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    const configPath = join(dir, 'desktop.json');

    const result = await saveDebugLoggingToggle({ enabled: true, configPath });

    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.settings.debug).toBe(true);
    const onDisk = JSON.parse(await readFile(configPath, 'utf-8'));
    expect(onDisk.logging).toEqual({ debug: true });
    await expect(loadRuntimeConfig({ isDev: false, configPath, env: {} })).resolves.toMatchObject({
      ok: true,
    });
  });

  it('round-trips the toggle without touching endpoints or operator fields', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    const configPath = join(dir, 'desktop.json');
    await writeFile(
      configPath,
      JSON.stringify({
        schemaVersion: 1,
        apiUrl: 'https://goosar.acme.test',
        telemetryOptOut: true,
        proxy: { host: '10.1.1.1', port: 9999 },
      }),
    );

    const on = await saveDebugLoggingToggle({ enabled: true, configPath });
    expect(on).toMatchObject({ ok: true, settings: { debug: true } });
    const off = await saveDebugLoggingToggle({ enabled: false, configPath });
    expect(off).toMatchObject({ ok: true, settings: { debug: false } });

    expect(JSON.parse(await readFile(configPath, 'utf-8'))).toEqual({
      schemaVersion: 1,
      apiUrl: 'https://goosar.acme.test',
      telemetryOptOut: true,
      proxy: { host: '10.1.1.1', port: 9999 },
      logging: { debug: false },
    });
  });

  it('refuses to replace a document it cannot parse', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    const configPath = join(dir, 'desktop.json');
    await writeFile(configPath, '{corrupted');

    const result = await saveDebugLoggingToggle({ enabled: true, configPath });

    expect(result.ok).toBe(false);
    expect(await readFile(configPath, 'utf-8')).toBe('{corrupted');
  });
});

describe('probeRuntimeServer', () => {
  it('upgrades a plain http remote address to https before probing', async () => {
    const fetchImpl = vi.fn().mockResolvedValue({ ok: true, status: 200 });

    const result = await probeRuntimeServer({
      apiUrl: 'http://goosar.ru',
      fetchImpl,
    });

    expect(result).toEqual({ ok: true, address: 'https://goosar.ru' });
    expect(fetchImpl.mock.calls[0]?.[0]).toBe('https://goosar.ru/health');
  });

  it('treats a bare host with no scheme as https before probing', async () => {
    const fetchImpl = vi.fn().mockResolvedValue({ ok: true, status: 200 });

    const result = await probeRuntimeServer({
      apiUrl: 'goosar.ru',
      fetchImpl,
    });

    expect(result).toEqual({ ok: true, address: 'https://goosar.ru' });
    expect(fetchImpl.mock.calls[0]?.[0]).toBe('https://goosar.ru/health');
  });

  it('leaves a loopback http address on http when probing', async () => {
    const fetchImpl = vi.fn().mockResolvedValue({ ok: true, status: 200 });

    const result = await probeRuntimeServer({
      apiUrl: 'http://localhost:8080',
      fetchImpl,
    });

    expect(result).toEqual({ ok: true, address: 'http://localhost:8080' });
    expect(fetchImpl.mock.calls[0]?.[0]).toBe('http://localhost:8080/health');
  });

  it('reports the address as reachable when the server answers /health', async () => {
    const fetchImpl = vi.fn().mockResolvedValue({ ok: true, status: 200 });

    const result = await probeRuntimeServer({
      apiUrl: 'https://goosar.acme.test/',
      fetchImpl,
    });

    expect(result).toEqual({ ok: true, address: 'https://goosar.acme.test' });
    expect(fetchImpl.mock.calls[0]?.[0]).toBe('https://goosar.acme.test/health');
  });

  it('names the address when the host does not resolve', async () => {
    const fetchImpl = vi
      .fn()
      .mockRejectedValue(new Error('getaddrinfo ENOTFOUND goosar.acme.test'));

    const result = await probeRuntimeServer({
      apiUrl: 'https://goosar.acme.test',
      fetchImpl,
    });

    expect(result.ok).toBe(false);
    expect(result.address).toBe('https://goosar.acme.test');
    if (!result.ok) {
      expect(result.message).toContain('ENOTFOUND');
    }
  });

  it('names the address when the server answers with an error status', async () => {
    const fetchImpl = vi.fn().mockResolvedValue({ ok: false, status: 502 });

    const result = await probeRuntimeServer({
      apiUrl: 'https://goosar.acme.test',
      fetchImpl,
    });

    expect(result.ok).toBe(false);
    expect(result.address).toBe('https://goosar.acme.test');
    if (!result.ok) {
      expect(result.message).toContain('502');
    }
  });

  it('names the address when the probe times out', async () => {
    const fetchImpl = vi.fn(
      (_url: string, init: { signal: AbortSignal }) =>
        new Promise<{ ok: boolean; status: number }>((_resolve, reject) => {
          init.signal.addEventListener('abort', () => {
            reject(new DOMException('aborted', 'AbortError'));
          });
        }),
    );

    const result = await probeRuntimeServer({
      apiUrl: 'https://goosar.acme.test',
      fetchImpl,
      timeoutMs: 10,
    });

    expect(result.ok).toBe(false);
    expect(result.address).toBe('https://goosar.acme.test');
    if (!result.ok) {
      expect(result.message).toMatch(/10ms/);
    }
  });

  it('rejects an address that is not a valid http URL', async () => {
    const fetchImpl = vi.fn();

    const result = await probeRuntimeServer({
      apiUrl: 'wss://goosar.acme.test',
      fetchImpl,
    });

    expect(result.ok).toBe(false);
    expect(result.address).toBe('wss://goosar.acme.test');
    if (!result.ok) {
      expect(result.message).toMatch(/http or https/);
    }
    expect(fetchImpl).not.toHaveBeenCalled();
  });
});

describe('loadRuntimeConfig with baked deployment defaults (#438)', () => {
  const baked = {
    schemaVersion: 1,
    apiUrl: 'https://goosar.corp.example',
    perimeter: { realm: 'CORP.EXAMPLE.COM' },
  };

  it('uses the baked defaults when the user has no desktop.json', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    await expect(
      loadRuntimeConfig({
        isDev: false,
        env: {},
        configPath: join(dir, 'desktop.json'),
        deploymentDefaults: baked,
      }),
    ).resolves.toEqual({
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: 'https://goosar.corp.example',
        wsUrl: 'wss://goosar.corp.example/ws',
        appUrl: 'https://goosar.corp.example',
      },
    });
  });

  it("lets the user's desktop.json win over the baked defaults", async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    const configPath = join(dir, 'desktop.json');
    await writeFile(
      configPath,
      JSON.stringify({ schemaVersion: 1, apiUrl: 'https://mine.example' }),
    );
    const result = await loadRuntimeConfig({
      isDev: false,
      env: {},
      configPath,
      deploymentDefaults: baked,
    });
    expect(result).toEqual({
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: 'https://mine.example',
        wsUrl: 'wss://mine.example/ws',
        appUrl: 'https://mine.example',
      },
    });
  });

  it('falls back to the shipped default when there is neither a user file nor baked defaults', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    await expect(
      loadRuntimeConfig({
        isDev: false,
        env: {},
        configPath: join(dir, 'desktop.json'),
        deploymentDefaults: null,
      }),
    ).resolves.toEqual({ ok: true, config: { ...DEFAULT_RUNTIME_CONFIG } });
  });

  it('falls back to the shipped default when the baked document is unusable', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    await expect(
      loadRuntimeConfig({
        isDev: false,
        env: {},
        configPath: join(dir, 'desktop.json'),
        deploymentDefaults: { schemaVersion: 1 },
      }),
    ).resolves.toEqual({ ok: true, config: { ...DEFAULT_RUNTIME_CONFIG } });
  });

  it('drops a baked wsUrl/appUrl pin when the user names their own apiUrl', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    const configPath = join(dir, 'desktop.json');
    await writeFile(
      configPath,
      JSON.stringify({ schemaVersion: 1, apiUrl: 'https://mine.example' }),
    );

    await expect(
      loadRuntimeConfig({
        isDev: false,
        env: {},
        configPath,
        deploymentDefaults: {
          ...baked,
          wsUrl: 'wss://ws.corp.example/ws',
          appUrl: 'https://app.corp.example',
        },
      }),
    ).resolves.toEqual({
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: 'https://mine.example',
        wsUrl: 'wss://mine.example/ws',
        appUrl: 'https://mine.example',
      },
    });
  });

  it('ignores baked defaults that would invalidate a usable user document', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    const configPath = join(dir, 'desktop.json');
    await writeFile(configPath, JSON.stringify({ apiUrl: 'https://mine.example' }));

    await expect(
      loadRuntimeConfig({
        isDev: false,
        env: {},
        configPath,
        deploymentDefaults: { schemaVersion: 99 },
      }),
    ).resolves.toEqual({
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: 'https://mine.example',
        wsUrl: 'wss://mine.example/ws',
        appUrl: 'https://mine.example',
      },
    });
  });

  it("still reports the user's own document as invalid, defaults or not", async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-desktop-config-'));
    const configPath = join(dir, 'desktop.json');
    await writeFile(configPath, '{ truncated');
    const result = await loadRuntimeConfig({
      isDev: false,
      env: {},
      configPath,
      deploymentDefaults: baked,
    });
    expect(result.ok).toBe(false);
  });
});
