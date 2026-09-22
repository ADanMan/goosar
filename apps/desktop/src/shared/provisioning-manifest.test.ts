import { describe, expect, it } from 'vitest';
import {
  PACKAGE_MANIFEST_VERSION,
  parsePackageManifest,
  type PackageManifest,
} from './provisioning-manifest';

function validManifest(overrides: Record<string, unknown> = {}): unknown {
  return {
    schemaVersion: PACKAGE_MANIFEST_VERSION,
    name: 'outlook',
    version: '1.4.0',
    type: 'mcp-server',
    platform: 'darwin-arm64',
    sha256: 'a'.repeat(64),
    size: 1024,
    requires: ['runtime:python@3.11.9'],
    ...overrides,
  };
}

describe('parsePackageManifest', () => {
  it('accepts a valid manifest and returns a normalized copy', () => {
    const raw = validManifest();
    const parsed = parsePackageManifest(raw);
    expect(parsed).toEqual(raw);
    expect(parsed.requires).not.toBe((raw as PackageManifest).requires);
  });

  it("accepts an empty requires array and the '*' platform", () => {
    const raw = validManifest({ platform: '*', requires: [] });
    expect(parsePackageManifest(raw)).toEqual(raw);
  });

  it('rejects a non-object', () => {
    expect(() => parsePackageManifest(null)).toThrow(/must be a JSON object/);
    expect(() => parsePackageManifest('nope')).toThrow(/must be a JSON object/);
    expect(() => parsePackageManifest([])).toThrow(/must be a JSON object/);
  });

  it('rejects the wrong schemaVersion', () => {
    expect(() => parsePackageManifest(validManifest({ schemaVersion: 2 }))).toThrow(
      /schemaVersion/,
    );
  });

  it('rejects a missing or malformed name', () => {
    expect(() => parsePackageManifest(validManifest({ name: '' }))).toThrow(/name/);
    expect(() => parsePackageManifest(validManifest({ name: '.hidden' }))).toThrow(/name/);
    expect(() => parsePackageManifest(validManifest({ name: 'has space' }))).toThrow(/name/);
    expect(() => parsePackageManifest(validManifest({ name: undefined }))).toThrow(/name/);
  });

  it('rejects a malformed version', () => {
    expect(() => parsePackageManifest(validManifest({ version: '' }))).toThrow(/version/);
    expect(() => parsePackageManifest(validManifest({ version: '1.0 beta' }))).toThrow(/version/);
    expect(() => parsePackageManifest(validManifest({ version: '1.0@rc1' }))).toThrow(/version/);
  });

  it('rejects a bad type enum', () => {
    expect(() => parsePackageManifest(validManifest({ type: 'npm-package' }))).toThrow(
      /type must be one of/,
    );
  });

  it('rejects a bad platform enum', () => {
    expect(() => parsePackageManifest(validManifest({ platform: 'solaris-sparc' }))).toThrow(
      /platform must be one of/,
    );
  });

  it('rejects a short or non-hex sha256', () => {
    expect(() => parsePackageManifest(validManifest({ sha256: 'abc123' }))).toThrow(/sha256/);
    expect(() => parsePackageManifest(validManifest({ sha256: 'g'.repeat(64) }))).toThrow(/sha256/);
    expect(() => parsePackageManifest(validManifest({ sha256: 'A'.repeat(64) }))).toThrow(/sha256/);
  });

  it('rejects a negative or non-integer size', () => {
    expect(() => parsePackageManifest(validManifest({ size: -1 }))).toThrow(/size/);
    expect(() => parsePackageManifest(validManifest({ size: 1.5 }))).toThrow(/size/);
    expect(() => parsePackageManifest(validManifest({ size: '1024' }))).toThrow(/size/);
  });

  it('rejects a non-array requires', () => {
    expect(() =>
      parsePackageManifest(validManifest({ requires: 'runtime:python@3.11.9' })),
    ).toThrow(/requires must be an array/);
  });

  it('rejects a malformed requires entry', () => {
    expect(() => parsePackageManifest(validManifest({ requires: ['python@3.11.9'] }))).toThrow(
      /requires entries must have the shape/,
    );
    expect(() => parsePackageManifest(validManifest({ requires: ['runtime:python'] }))).toThrow(
      /requires entries must have the shape/,
    );
    expect(() =>
      parsePackageManifest(validManifest({ requires: ['bogus-type:python@3.11.9'] })),
    ).toThrow(/requires entries must have the shape/);
  });

  it('rejects a missing field', () => {
    const raw = validManifest() as Record<string, unknown>;
    delete raw.sha256;
    expect(() => parsePackageManifest(raw)).toThrow(/sha256/);
  });
});
