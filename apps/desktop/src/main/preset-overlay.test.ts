import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import {
  OVERLAY_FILENAME,
  loadPresetOverlay,
  parsePresetOverlay,
  presetOverlayStagedDir,
} from './preset-overlay';

let dir: string;

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), 'goosar-preset-overlay-'));
});

afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

describe('parsePresetOverlay', () => {
  it('returns the object for a valid JSON object', () => {
    expect(parsePresetOverlay('{"mcpServers":{"atlassian":{}}}')).toEqual({
      mcpServers: { atlassian: {} },
    });
  });

  it('returns null for non-JSON, arrays, strings, and numbers', () => {
    expect(parsePresetOverlay('not json')).toBeNull();
    expect(parsePresetOverlay('[1,2]')).toBeNull();
    expect(parsePresetOverlay('"a string"')).toBeNull();
    expect(parsePresetOverlay('42')).toBeNull();
    expect(parsePresetOverlay('null')).toBeNull();
  });
});

describe('loadPresetOverlay', () => {
  it('returns null when neither source is present', () => {
    expect(loadPresetOverlay({ stagedDir: dir })).toBeNull();
  });

  it('returns null when stagedDir is null and no env path', () => {
    expect(loadPresetOverlay({ stagedDir: null })).toBeNull();
  });

  it('reads the staged file', () => {
    writeFileSync(
      join(dir, OVERLAY_FILENAME),
      '{"mcpServers":{"outlook":{"env":{"EWS_SERVER_URL":"https://owa"}}}}',
    );
    expect(loadPresetOverlay({ stagedDir: dir })).toEqual({
      mcpServers: { outlook: { env: { EWS_SERVER_URL: 'https://owa' } } },
    });
  });

  it('prefers the env override path over the staged file', () => {
    const envFile = join(dir, 'override.json');
    writeFileSync(envFile, '{"from":"env"}');
    writeFileSync(join(dir, OVERLAY_FILENAME), '{"from":"staged"}');
    expect(loadPresetOverlay({ envPath: envFile, stagedDir: dir })).toEqual({
      from: 'env',
    });
  });

  it('falls through a malformed first candidate to the next', () => {
    const envFile = join(dir, 'bad.json');
    writeFileSync(envFile, 'not json');
    writeFileSync(join(dir, OVERLAY_FILENAME), '{"from":"staged"}');
    expect(loadPresetOverlay({ envPath: envFile, stagedDir: dir })).toEqual({
      from: 'staged',
    });
  });
});

describe('presetOverlayStagedDir', () => {
  it('returns the packaged dir when it exists', () => {
    mkdirSync(join(dir, 'preset-overlay'));
    expect(
      presetOverlayStagedDir({
        isPackaged: true,
        resourcesPath: dir,
        appPath: '/nope',
      }),
    ).toBe(join(dir, 'preset-overlay'));
  });

  it('returns the dev dir when it exists', () => {
    mkdirSync(join(dir, 'resources-preset-overlay'));
    expect(
      presetOverlayStagedDir({
        isPackaged: false,
        resourcesPath: '/nope',
        appPath: dir,
      }),
    ).toBe(join(dir, 'resources-preset-overlay'));
  });

  it('returns null when the dir is absent', () => {
    expect(
      presetOverlayStagedDir({
        isPackaged: true,
        resourcesPath: dir,
        appPath: dir,
      }),
    ).toBeNull();
  });
});
