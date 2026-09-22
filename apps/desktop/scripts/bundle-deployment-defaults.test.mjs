import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { spawnSync } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { normalizeDeploymentDefaults } from './bundle-deployment-defaults.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const script = join(here, 'bundle-deployment-defaults.mjs');
const desktopRoot = resolve(here, '..');
const stagingDir = join(desktopRoot, 'resources-deployment');
const stagedFile = join(stagingDir, 'deployment.json');
const marker = join(stagingDir, 'DEPLOYMENT-DEFAULTS-NOT-BUNDLED.txt');

let work;

function runScript(env = {}) {
  const result = spawnSync('node', [script], {
    cwd: desktopRoot,
    encoding: 'utf-8',
    env: { ...process.env, ...env },
  });
  return {
    status: result.status,
    output: `${result.stdout ?? ''}${result.stderr ?? ''}`,
  };
}

beforeEach(() => {
  work = mkdtempSync(join(tmpdir(), 'bundle-deployment-defaults-'));
  rmSync(stagingDir, { recursive: true, force: true });
});

afterEach(() => {
  rmSync(work, { recursive: true, force: true });
  rmSync(stagingDir, { recursive: true, force: true });
});

describe('normalizeDeploymentDefaults', () => {
  it('re-serializes a valid document with a trailing newline', () => {
    const { parsed, json } = normalizeDeploymentDefaults(
      '{"apiUrl":"https://goosar.corp.example"}',
    );
    expect(parsed).toEqual({ apiUrl: 'https://goosar.corp.example' });
    expect(json.endsWith('\n')).toBe(true);
  });

  it('carries the deployment host block through untouched', () => {
    const { parsed } = normalizeDeploymentDefaults(
      JSON.stringify({
        apiUrl: 'https://goosar.example.test',
        perimeter: { realm: 'EXAMPLE.TEST' },
        deployment: {
          jiraUrl: 'https://jira.example.test',
          confluenceUrl: 'https://wiki.example.test',
          ewsUrl: 'https://mail.example.test/EWS/Exchange.asmx',
          mailDomain: 'example.test',
          llmApiBase: 'https://llm.example.test/v1',
          llmModel: 'openai/coding-medium',
        },
      }),
    );
    expect(parsed.deployment).toEqual({
      jiraUrl: 'https://jira.example.test',
      confluenceUrl: 'https://wiki.example.test',
      ewsUrl: 'https://mail.example.test/EWS/Exchange.asmx',
      mailDomain: 'example.test',
      llmApiBase: 'https://llm.example.test/v1',
      llmModel: 'openai/coding-medium',
    });
    expect(parsed.perimeter).toEqual({ realm: 'EXAMPLE.TEST' });
  });

  it('rejects unparseable JSON and non-object top levels', () => {
    expect(() => normalizeDeploymentDefaults('{ truncated')).toThrow(/not valid JSON/);
    expect(() => normalizeDeploymentDefaults('[1,2]')).toThrow(/must be a JSON object/);
  });
});

describe('bundle-deployment-defaults CLI', () => {
  it('writes only a marker when the source env is unset', () => {
    const { status } = runScript({
      GOOSAR_DESKTOP_DEPLOYMENT_DEFAULTS: '',
    });
    expect(status).toBe(0);
    expect(existsSync(marker)).toBe(true);
    expect(existsSync(stagedFile)).toBe(false);
  });

  it('stages a normalized copy when the source is valid', () => {
    const source = join(work, 'deployment.json');
    writeFileSync(
      source,
      '{"schemaVersion":1,"apiUrl":"https://goosar.corp.example","perimeter":{"realm":"CORP.EXAMPLE.COM"}}',
    );
    const { status } = runScript({
      GOOSAR_DESKTOP_DEPLOYMENT_DEFAULTS: source,
    });
    expect(status).toBe(0);
    expect(JSON.parse(readFileSync(stagedFile, 'utf-8'))).toEqual({
      schemaVersion: 1,
      apiUrl: 'https://goosar.corp.example',
      perimeter: { realm: 'CORP.EXAMPLE.COM' },
    });
    expect(existsSync(marker)).toBe(false);
  });

  it('fails the build when the source path does not exist', () => {
    const { status, output } = runScript({
      GOOSAR_DESKTOP_DEPLOYMENT_DEFAULTS: join(work, 'nope.json'),
    });
    expect(status).toBe(1);
    expect(output).toContain('does not exist');
  });

  it('fails the build when the source is not readable JSON', () => {
    const source = join(work, 'broken.json');
    writeFileSync(source, '{ truncated');
    const { status, output } = runScript({
      GOOSAR_DESKTOP_DEPLOYMENT_DEFAULTS: source,
    });
    expect(status).toBe(1);
    expect(output).toContain('not valid JSON');
  });
});
