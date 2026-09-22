import { describe, expect, it } from 'vitest';
import { matchLocale, pickLocale } from './pick-locale';
import type { LocaleAdapter } from './types';

function makeAdapter(overrides: Partial<LocaleAdapter> = {}): LocaleAdapter {
  return {
    getUserChoice: () => null,
    persist: () => {},
    ...overrides,
  };
}

describe('matchLocale', () => {
  it('returns DEFAULT_LOCALE when given an empty list', () => {
    expect(matchLocale([])).toBe('ru');
  });

  it('matches a clean supported tag', () => {
    expect(matchLocale(['zh-Hans'])).toBe('zh-Hans');
    expect(matchLocale(['ko'])).toBe('ko');
    expect(matchLocale(['ja'])).toBe('ja');
    expect(matchLocale(['ru'])).toBe('ru');
    expect(matchLocale(['en'])).toBe('en');
  });

  it('collapses region-tagged BCP-47 to the supported base', () => {
    expect(matchLocale(['en-US'])).toBe('en');
    expect(matchLocale(['zh-Hans-CN'])).toBe('zh-Hans');
    expect(matchLocale(['ko-KR'])).toBe('ko');
    expect(matchLocale(['ja-JP'])).toBe('ja');
    expect(matchLocale(['ru-RU'])).toBe('ru');
  });

  it('falls back to DEFAULT_LOCALE when no candidate matches', () => {
    expect(matchLocale(['fr', 'de'])).toBe('ru');
  });

  it('zh-Hant (traditional) collapses to zh-Hans — same base subtag, better UX than English fallback', () => {
    expect(matchLocale(['zh-Hant'])).toBe('zh-Hans');
  });

  it('uses the first supported candidate when multiple appear', () => {
    expect(matchLocale(['fr', 'zh-Hans', 'en'])).toBe('zh-Hans');
    expect(matchLocale(['fr', 'ko-KR', 'en'])).toBe('ko');
    expect(matchLocale(['fr', 'ja-JP', 'en'])).toBe('ja');
  });

  it('returns DEFAULT_LOCALE for malformed BCP-47 tags rather than throwing', () => {
    expect(matchLocale(['----'])).toBe('ru');
    expect(matchLocale(['x-private-only'])).toBe('ru');
  });
});

describe('pickLocale', () => {
  it('prefers explicit user choice over the default', () => {
    const adapter = makeAdapter({ getUserChoice: () => 'zh-Hans' });
    expect(pickLocale(adapter)).toBe('zh-Hans');
  });

  it('keeps an explicitly stored English choice', () => {
    const adapter = makeAdapter({ getUserChoice: () => 'en' });
    expect(pickLocale(adapter)).toBe('en');
  });

  it('returns DEFAULT_LOCALE when there is no explicit user choice', () => {
    expect(pickLocale(makeAdapter())).toBe('ru');
  });

  it('ignores an empty-string user choice and returns DEFAULT_LOCALE', () => {
    const adapter = makeAdapter({ getUserChoice: () => '' });
    expect(pickLocale(adapter)).toBe('ru');
  });

  it('returns DEFAULT_LOCALE when the stored choice matches nothing', () => {
    const adapter = makeAdapter({ getUserChoice: () => 'fr' });
    expect(pickLocale(adapter)).toBe('ru');
  });
});
