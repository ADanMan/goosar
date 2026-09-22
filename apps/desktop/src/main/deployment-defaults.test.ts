import { mkdtemp, writeFile } from 'fs/promises';
import { join } from 'path';
import { tmpdir } from 'os';
import { describe, expect, it } from 'vitest';
import {
  DEPLOYMENT_DEFAULTS_FILENAME,
  loadDeploymentDefaults,
  parseDeploymentDefaults,
} from './deployment-defaults';

describe('parseDeploymentDefaults', () => {
  it('returns the object for a valid document', () => {
    expect(parseDeploymentDefaults('{"apiUrl":"https://goosar.corp"}')).toEqual({
      apiUrl: 'https://goosar.corp',
    });
  });

  it('returns null for malformed JSON and for non-objects', () => {
    expect(parseDeploymentDefaults('{not json')).toBeNull();
    expect(parseDeploymentDefaults('[1,2]')).toBeNull();
    expect(parseDeploymentDefaults('"a string"')).toBeNull();
  });
});

describe('loadDeploymentDefaults', () => {
  it('returns null when nothing is staged and no env path is set', () => {
    expect(loadDeploymentDefaults({ stagedDir: null })).toBeNull();
  });

  it('reads the staged file', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-deployment-'));
    await writeFile(
      join(dir, DEPLOYMENT_DEFAULTS_FILENAME),
      '{"schemaVersion":1,"apiUrl":"https://goosar.corp"}',
    );
    expect(loadDeploymentDefaults({ stagedDir: dir })).toEqual({
      schemaVersion: 1,
      apiUrl: 'https://goosar.corp',
    });
  });

  it('prefers the env path over the staged file', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-deployment-'));
    await writeFile(join(dir, DEPLOYMENT_DEFAULTS_FILENAME), '{"apiUrl":"https://staged.corp"}');
    const envFile = join(dir, 'other.json');
    await writeFile(envFile, '{"apiUrl":"https://env.corp"}');
    expect(loadDeploymentDefaults({ envPath: envFile, stagedDir: dir })).toEqual({
      apiUrl: 'https://env.corp',
    });
  });

  it('falls through a malformed staged file instead of throwing', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'goosar-deployment-'));
    await writeFile(join(dir, DEPLOYMENT_DEFAULTS_FILENAME), '{ truncated');
    expect(loadDeploymentDefaults({ stagedDir: dir })).toBeNull();
  });
});
