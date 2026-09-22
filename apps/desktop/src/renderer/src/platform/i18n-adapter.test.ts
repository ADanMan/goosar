import { beforeEach, describe, expect, it } from 'vitest';
import { pickLocale } from '@goosar/core/i18n';
import { createDesktopLocaleAdapter } from './i18n-adapter';

const STORAGE_KEY = 'goosar-locale';
const EXPLICIT_KEY = 'goosar-locale-explicit';

describe('createDesktopLocaleAdapter', () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it('resolves the Russian default on a clean profile', () => {
    expect(pickLocale(createDesktopLocaleAdapter())).toBe('ru');
  });

  it('ignores a locale the old OS-language effect left behind', () => {
    window.localStorage.setItem(STORAGE_KEY, 'en');

    expect(createDesktopLocaleAdapter().getUserChoice()).toBe(null);
    expect(pickLocale(createDesktopLocaleAdapter())).toBe('ru');
  });

  it('ignores every inherited system locale, not just English', () => {
    for (const inherited of ['en', 'ja', 'ko', 'zh-Hans']) {
      window.localStorage.clear();
      window.localStorage.setItem(STORAGE_KEY, inherited);
      expect(pickLocale(createDesktopLocaleAdapter())).toBe('ru');
    }
  });

  it('keeps an explicitly stored English choice', () => {
    createDesktopLocaleAdapter().persist('en');
    expect(pickLocale(createDesktopLocaleAdapter())).toBe('en');
  });

  it('keeps the other explicitly stored choices', () => {
    for (const stored of ['zh-Hans', 'ko', 'ja', 'ru'] as const) {
      window.localStorage.clear();
      createDesktopLocaleAdapter().persist(stored);
      expect(pickLocale(createDesktopLocaleAdapter())).toBe(stored);
    }
  });

  it('falls back to the Russian default for an unsupported stored value', () => {
    window.localStorage.setItem(STORAGE_KEY, 'fr');
    window.localStorage.setItem(EXPLICIT_KEY, '1');
    expect(pickLocale(createDesktopLocaleAdapter())).toBe('ru');
  });

  it('persists a chosen locale so the next boot reads it back', () => {
    createDesktopLocaleAdapter().persist('en');
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('en');
    expect(createDesktopLocaleAdapter().getUserChoice()).toBe('en');
    expect(pickLocale(createDesktopLocaleAdapter())).toBe('en');
  });

  it('ignores a marker left without a locale', () => {
    window.localStorage.setItem(EXPLICIT_KEY, '1');
    expect(createDesktopLocaleAdapter().getUserChoice()).toBe(null);
    expect(pickLocale(createDesktopLocaleAdapter())).toBe('ru');
  });

  it('takes no OS-language input and exposes no system-preference channel', () => {
    expect(createDesktopLocaleAdapter.length).toBe(0);
    expect(Object.keys(createDesktopLocaleAdapter()).sort()).toEqual(['getUserChoice', 'persist']);
  });
});
