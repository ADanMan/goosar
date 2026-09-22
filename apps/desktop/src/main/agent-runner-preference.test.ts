import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { loadAgentRunner, normalizeAgentRunner, saveAgentRunner } from './agent-runner-preference';

let dir: string;
let file: string;

beforeEach(async () => {
  dir = await mkdtemp(join(tmpdir(), 'agent-runner-pref-'));
  file = join(dir, 'nested', 'desktop_prefs.json');
});

afterEach(async () => {
  await rm(dir, { recursive: true, force: true });
});

describe('normalizeAgentRunner', () => {
  it('keeps the known choices', () => {
    expect(normalizeAgentRunner('none')).toBe('none');
    expect(normalizeAgentRunner('hermes')).toBe('hermes');
  });

  it.each([undefined, null, '', 'HERMES', 'openclaw', 1, true, {}, ['hermes']])(
    'reads %j as none',
    (value) => {
      expect(normalizeAgentRunner(value)).toBe('none');
    },
  );
});

describe('loadAgentRunner', () => {
  it('defaults to none when the file is missing', async () => {
    expect(await loadAgentRunner(file)).toBe('none');
  });

  it('defaults to none for corrupt or non-object content', async () => {
    await writeFile(join(dir, 'a.json'), '{not json', 'utf-8');
    await writeFile(join(dir, 'b.json'), '["hermes"]', 'utf-8');
    expect(await loadAgentRunner(join(dir, 'a.json'))).toBe('none');
    expect(await loadAgentRunner(join(dir, 'b.json'))).toBe('none');
  });

  it('normalises an unknown persisted value to none', async () => {
    const path = join(dir, 'c.json');
    await writeFile(path, JSON.stringify({ autoStart: true, agentRunner: 'legacy' }), 'utf-8');
    expect(await loadAgentRunner(path)).toBe('none');
  });

  it('reads a persisted hermes choice', async () => {
    const path = join(dir, 'd.json');
    await writeFile(path, JSON.stringify({ agentRunner: 'hermes' }), 'utf-8');
    expect(await loadAgentRunner(path)).toBe('hermes');
  });
});

describe('saveAgentRunner', () => {
  it('creates the file and round-trips the choice', async () => {
    await saveAgentRunner(file, 'hermes');
    expect(await loadAgentRunner(file)).toBe('hermes');
    await saveAgentRunner(file, 'none');
    expect(await loadAgentRunner(file)).toBe('none');
  });

  it('keeps the daemon preferences that share the file', async () => {
    const path = join(dir, 'e.json');
    await writeFile(path, JSON.stringify({ autoStart: false, autoStop: true }), 'utf-8');

    await saveAgentRunner(path, 'hermes');

    expect(JSON.parse(await readFile(path, 'utf-8'))).toEqual({
      autoStart: false,
      autoStop: true,
      agentRunner: 'hermes',
    });
  });
});
