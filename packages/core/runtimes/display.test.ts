import { describe, expect, it } from 'vitest';
import { runtimeDisplayLabel, runtimeDisplayName } from './display';

describe('runtimeDisplayName', () => {
  it('prefers a custom name when set', () => {
    expect(runtimeDisplayName({ name: 'Claude (host)', custom_name: 'Prod Box' })).toBe('Prod Box');
  });

  it('trims the custom name', () => {
    expect(runtimeDisplayName({ name: 'Claude (host)', custom_name: '  Prod Box  ' })).toBe(
      'Prod Box',
    );
  });

  it('falls back to the default name when custom is empty, whitespace, null, or missing', () => {
    expect(runtimeDisplayName({ name: 'Claude (host)', custom_name: '' })).toBe('Claude (host)');
    expect(runtimeDisplayName({ name: 'Claude (host)', custom_name: '   ' })).toBe('Claude (host)');
    expect(runtimeDisplayName({ name: 'Claude (host)', custom_name: null })).toBe('Claude (host)');
    expect(runtimeDisplayName({ name: 'Claude (host)' })).toBe('Claude (host)');
  });
});

describe('runtimeDisplayLabel', () => {
  it('re-attaches the provider when a custom alias hides it', () => {
    expect(
      runtimeDisplayLabel({
        name: 'Codex (EvaM2.local)',
        custom_name: 'evam2',
        provider: 'codex',
      }),
    ).toBe('evam2 (Codex)');
  });

  it('returns the daemon name unchanged when no alias is set', () => {
    expect(
      runtimeDisplayLabel({
        name: 'Codex (EvaM2.local)',
        custom_name: '',
        provider: 'codex',
      }),
    ).toBe('Codex (EvaM2.local)');
    expect(
      runtimeDisplayLabel({
        name: 'Codex (EvaM2.local)',
        custom_name: null,
        provider: 'codex',
      }),
    ).toBe('Codex (EvaM2.local)');
  });

  it('omits the provider suffix when the provider is empty', () => {
    expect(runtimeDisplayLabel({ name: 'host', custom_name: 'evam2', provider: '' })).toBe('evam2');
  });

  it("formats a runtime code into the registry's generic 'Runtime <LETTER>' pattern", () => {
    expect(
      runtimeDisplayLabel({
        name: 'Runtime R (host)',
        custom_name: 'box',
        provider: 'runtime-r',
      }),
    ).toBe('box (Runtime R)');
    expect(
      runtimeDisplayLabel({
        name: 'Runtime Q (host)',
        custom_name: 'box',
        provider: 'runtime-q',
      }),
    ).toBe('box (Runtime Q)');
  });

  it('first-letter-capitalizes a hyphen-less slug', () => {
    expect(
      runtimeDisplayLabel({
        name: 'Openclaw (host)',
        custom_name: 'box',
        provider: 'openclaw',
      }),
    ).toBe('box (Openclaw)');
    expect(
      runtimeDisplayLabel({
        name: 'Codex (host)',
        custom_name: 'box',
        provider: 'codex',
      }),
    ).toBe('box (Codex)');
  });
});
