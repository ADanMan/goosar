import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import {
  caBundleFilePath,
  corpCaFilePath,
  ensureCaBundleFiles,
  readCaBundleStatus,
  type AgentPathContext,
} from './agent-bootstrap';

let home: string;
let bundled: string;

function ctx(overrides: Partial<AgentPathContext> = {}): AgentPathContext {
  return { home, env: { HOME: home }, ...overrides };
}

function stageBundledCa(files: Partial<Record<string, string>>): void {
  mkdirSync(bundled, { recursive: true });
  for (const [name, content] of Object.entries(files)) {
    writeFileSync(join(bundled, name), content ?? '');
  }
}

beforeEach(() => {
  home = mkdtempSync(join(tmpdir(), 'goosar-ca-home-'));
  bundled = mkdtempSync(join(tmpdir(), 'goosar-ca-bundled-'));
});

afterEach(() => {
  rmSync(home, { recursive: true, force: true });
  rmSync(bundled, { recursive: true, force: true });
});

describe('readCaBundleStatus', () => {
  it('reports both files missing on a fresh machine', () => {
    expect(readCaBundleStatus(ctx())).toEqual({
      caBundlePresent: false,
      corpCaPresent: false,
    });
  });

  it('reports presence per file', () => {
    mkdirSync(join(home, '.hermes'), { recursive: true });
    writeFileSync(join(home, '.hermes', 'ca-bundle.pem'), 'PEM');
    expect(readCaBundleStatus(ctx())).toEqual({
      caBundlePresent: true,
      corpCaPresent: false,
    });
  });

  it('honors the HERMES_HOME override like the rest of the bootstrap', () => {
    const override = join(home, 'custom-agent-home');
    mkdirSync(override, { recursive: true });
    writeFileSync(join(override, 'corp-ca.pem'), 'PEM');
    const c = ctx({ env: { HOME: home, HERMES_HOME: override } });
    expect(caBundleFilePath(c)).toBe(join(override, 'ca-bundle.pem'));
    expect(readCaBundleStatus(c)).toEqual({
      caBundlePresent: false,
      corpCaPresent: true,
    });
  });
});

describe('ensureCaBundleFiles', () => {
  it('materializes both bundled files into a fresh agent home', async () => {
    stageBundledCa({
      'ca-bundle.pem': 'FULL-BUNDLE',
      'corp-ca.pem': 'CORP-ONLY',
    });

    const status = await ensureCaBundleFiles(ctx(), bundled);

    expect(status).toEqual({ caBundlePresent: true, corpCaPresent: true });
    expect(readFileSync(caBundleFilePath(ctx()), 'utf-8')).toBe('FULL-BUNDLE');
    expect(readFileSync(corpCaFilePath(ctx()), 'utf-8')).toBe('CORP-ONLY');
  });

  it('NEVER overwrites an existing file', async () => {
    stageBundledCa({
      'ca-bundle.pem': 'SHIPPED',
      'corp-ca.pem': 'SHIPPED',
    });
    mkdirSync(join(home, '.hermes'), { recursive: true });
    writeFileSync(join(home, '.hermes', 'ca-bundle.pem'), 'OPERATOR-OWNED');

    const status = await ensureCaBundleFiles(ctx(), bundled);

    expect(status).toEqual({ caBundlePresent: true, corpCaPresent: true });
    expect(readFileSync(caBundleFilePath(ctx()), 'utf-8')).toBe('OPERATOR-OWNED');
    expect(readFileSync(corpCaFilePath(ctx()), 'utf-8')).toBe('SHIPPED');
  });

  it('is a clean no-op when this build carries no CA payload', async () => {
    const status = await ensureCaBundleFiles(ctx(), null);
    expect(status).toEqual({ caBundlePresent: false, corpCaPresent: false });
  });

  it('copies only the files the payload actually has', async () => {
    stageBundledCa({ 'ca-bundle.pem': 'FULL-BUNDLE' });
    const status = await ensureCaBundleFiles(ctx(), bundled);
    expect(status).toEqual({ caBundlePresent: true, corpCaPresent: false });
  });

  it('is idempotent across repeated launches', async () => {
    stageBundledCa({ 'ca-bundle.pem': 'V1', 'corp-ca.pem': 'V1' });
    await ensureCaBundleFiles(ctx(), bundled);

    writeFileSync(join(bundled, 'ca-bundle.pem'), 'V2');
    const status = await ensureCaBundleFiles(ctx(), bundled);

    expect(status).toEqual({ caBundlePresent: true, corpCaPresent: true });
    expect(readFileSync(caBundleFilePath(ctx()), 'utf-8')).toBe('V1');
  });
});
