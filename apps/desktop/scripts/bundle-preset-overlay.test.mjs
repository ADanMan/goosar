import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { spawnSync } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { normalizeOverlay } from './bundle-preset-overlay.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const script = join(here, 'bundle-preset-overlay.mjs');
const desktopRoot = resolve(here, '..');
const stagingDir = join(desktopRoot, 'resources-preset-overlay');
const stagedFile = join(stagingDir, 'preset-overlay.json');
const manifestFile = join(stagingDir, 'PRESET-OVERLAY-MANIFEST.txt');
const marker = join(stagingDir, 'PRESET-OVERLAY-NOT-BUNDLED.txt');

let work;

function runScript(env = {}, args = []) {
  const result = spawnSync('node', [script, ...args], {
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
  work = mkdtempSync(join(tmpdir(), 'bundle-preset-overlay-'));
  rmSync(stagingDir, { recursive: true, force: true });
});

afterEach(() => {
  rmSync(work, { recursive: true, force: true });
  rmSync(stagingDir, { recursive: true, force: true });
});

describe('normalizeOverlay', () => {
  it('re-serializes a valid overlay object with a trailing newline', () => {
    const { parsed, json } = normalizeOverlay(
      '{"mcpServers":{"atlassian":{"env":{"JIRA_URL":"https://x"}}}}',
    );
    expect(parsed).toEqual({
      mcpServers: { atlassian: { env: { JIRA_URL: 'https://x' } } },
    });
    expect(json.endsWith('\n')).toBe(true);
    expect(json).toContain('"mcpServers"');
  });

  it('throws on invalid JSON', () => {
    expect(() => normalizeOverlay('not json')).toThrow(/not valid JSON/);
  });

  it('throws when the top level is not an object', () => {
    expect(() => normalizeOverlay('[1,2,3]')).toThrow(/must be a JSON object/);
    expect(() => normalizeOverlay('"a string"')).toThrow(/must be a JSON object/);
  });
});

describe('bundle-preset-overlay CLI', () => {
  it('stages nothing and writes a marker when the source env is unset', () => {
    const { status, output } = runScript({ GOOSAR_PRESET_OVERLAY: '' });
    expect(status).toBe(0);
    expect(output).toMatch(/building without a preset overlay/);
    expect(existsSync(marker)).toBe(true);
    expect(existsSync(stagedFile)).toBe(false);
  });

  it('hard-fails when unset but --require-preset-overlay is passed', () => {
    const { status, output } = runScript({ GOOSAR_PRESET_OVERLAY: '' }, [
      '--require-preset-overlay',
    ]);
    expect(status).toBe(1);
    expect(output).toMatch(/refusing to build without the MCP preset overlay/);
  });

  it('hard-fails when the source env points at a non-existent file', () => {
    const { status, output } = runScript({
      GOOSAR_PRESET_OVERLAY: join(work, 'nope.json'),
    });
    expect(status).toBe(1);
    expect(output).toMatch(/does not exist/);
  });

  it('hard-fails on a malformed overlay file and stages nothing', () => {
    const bad = join(work, 'bad.json');
    writeFileSync(bad, 'not json');
    const { status, output } = runScript({ GOOSAR_PRESET_OVERLAY: bad });
    expect(status).toBe(1);
    expect(output).toMatch(/not valid JSON/);
    expect(existsSync(stagedFile)).toBe(false);
  });

  it('refuses an overlay that bakes a credential, unless allowed explicitly', () => {
    const src = join(work, 'secret-overlay.json');
    writeFileSync(
      src,
      JSON.stringify({
        mcpServers: {
          'mcp-gateway': {
            args: ['-H', 'Authorization', 'Bearer real-token', 'https://gw.corp'],
          },
        },
      }),
    );

    const refused = runScript({ GOOSAR_PRESET_OVERLAY: src });
    expect(refused.status).toBe(1);
    expect(refused.output).toMatch(/credential/);
    expect(existsSync(stagedFile)).toBe(false);

    const allowed = runScript({ GOOSAR_PRESET_OVERLAY: src }, ['--allow-preset-overlay-secrets']);
    expect(allowed.status).toBe(0);
    expect(existsSync(stagedFile)).toBe(true);

    rmSync(stagedFile, { force: true });
    const allowedByEnv = runScript({
      GOOSAR_PRESET_OVERLAY: src,
      GOOSAR_PRESET_OVERLAY_ALLOW_SECRETS: '1',
    });
    expect(allowedByEnv.status).toBe(0);
    expect(existsSync(stagedFile)).toBe(true);
  });

  it('stages a normalized copy and a manifest for a valid overlay', () => {
    const src = join(work, 'overlay.json');
    writeFileSync(
      src,
      JSON.stringify({
        mcpServers: { outlook: { env: { EWS_SERVER_URL: 'https://owa.corp' } } },
      }),
    );

    const { status, output } = runScript({ GOOSAR_PRESET_OVERLAY: src });
    expect(status).toBe(0);
    expect(output).toMatch(/staged overlay/);

    expect(existsSync(stagedFile)).toBe(true);
    expect(JSON.parse(readFileSync(stagedFile, 'utf-8'))).toEqual({
      mcpServers: { outlook: { env: { EWS_SERVER_URL: 'https://owa.corp' } } },
    });
    const manifest = readFileSync(manifestFile, 'utf-8');
    expect(manifest).toMatch(/sha256=[0-9a-f]{64}/);
  });
});
