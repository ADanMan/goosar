import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import type { AgentPathContext } from './agent-bootstrap';
import { writeConfigFieldsDirectly, writeLlmFieldsDirectly } from './agent-config-yaml-fallback';

let home: string;

function ctxFor(): AgentPathContext {
  return { home, env: { HOME: home } };
}

const SEEDED_CONFIG = `llm:
  api_base: https://old.example/v1
  model: openai/old-model
  api_key: OLD_KEY
mcp_servers:
  ews-mcp:
    stdio: {}
security:
  bash_full: false
`;

function seedConfig(): string {
  const dir = join(home, '.hermes');
  mkdirSync(dir, { recursive: true });
  const path = join(dir, 'config.user.yaml');
  writeFileSync(path, SEEDED_CONFIG);
  return path;
}

beforeEach(() => {
  home = mkdtempSync(join(tmpdir(), 'hermes-yaml-fallback-'));
});

afterEach(() => {
  rmSync(home, { recursive: true, force: true });
});

describe('writeConfigFieldsDirectly', () => {
  it('writes a nested dotted key and preserves the rest of the document', async () => {
    const path = seedConfig();

    const result = await writeConfigFieldsDirectly(ctxFor(), {
      'security.bash_full': true,
    });

    expect(result).toEqual({ ok: true, path });

    const text = readFileSync(path, 'utf-8');
    expect(text).toContain('bash_full: true');
    expect(text).toContain('api_base: https://old.example/v1');
    expect(text).toContain('ews-mcp');
  });

  it('writes atomically at mode 0600', async () => {
    const path = seedConfig();

    await writeConfigFieldsDirectly(ctxFor(), { 'security.bash_full': true });

    const mode = statSync(path).mode & 0o777;
    expect(mode).toBe(0o600);
  });

  it('reports failure when no config file exists', async () => {
    const result = await writeConfigFieldsDirectly(ctxFor(), {
      'security.bash_full': true,
    });
    expect(result.ok).toBe(false);
    expect(result.message).toMatch(/no config\.user\.yaml/);
  });
});

describe('writeLlmFieldsDirectly (wrapper)', () => {
  it('writes only the llm.* string fields from the patch', async () => {
    const path = seedConfig();

    const result = await writeLlmFieldsDirectly(ctxFor(), {
      'llm.api_base': 'https://new.example/v1',
      'security.bash_full': true, // not a string -> ignored by the wrapper
    });

    expect(result.ok).toBe(true);
    const text = readFileSync(path, 'utf-8');
    expect(text).toContain('api_base: https://new.example/v1');
    expect(text).toContain('bash_full: false');
  });
});
