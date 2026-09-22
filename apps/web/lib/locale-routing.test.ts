import { describe, expect, it } from 'vitest';
import { isSupportedLocale, resolveLocaleFromCookie } from './locale-routing';

describe('locale routing', () => {
  it('accepts only app-supported locale identifiers', () => {
    expect(isSupportedLocale('en')).toBe(true);
    expect(isSupportedLocale('zh-Hans')).toBe(true);
    expect(isSupportedLocale('ko')).toBe(true);
    expect(isSupportedLocale('ja')).toBe(true);
    expect(isSupportedLocale('ru')).toBe(true);
    expect(isSupportedLocale('zh')).toBe(false);
    expect(isSupportedLocale(null)).toBe(false);
  });

  it('normalizes legacy landing zh cookies to the app locale', () => {
    expect(resolveLocaleFromCookie('zh')).toBe('zh-Hans');
  });

  it('honors an explicitly stored English cookie', () => {
    expect(resolveLocaleFromCookie('en')).toBe('en');
  });

  it('honors the other explicitly stored locales', () => {
    expect(resolveLocaleFromCookie('zh-Hans')).toBe('zh-Hans');
    expect(resolveLocaleFromCookie('ko')).toBe('ko');
    expect(resolveLocaleFromCookie('ja')).toBe('ja');
    expect(resolveLocaleFromCookie('ru')).toBe('ru');
  });

  it('falls back to the Russian default when no cookie is set', () => {
    expect(resolveLocaleFromCookie(undefined)).toBe('ru');
    expect(resolveLocaleFromCookie(null)).toBe('ru');
    expect(resolveLocaleFromCookie('')).toBe('ru');
  });

  it('falls back to the Russian default for an unsupported cookie value', () => {
    expect(resolveLocaleFromCookie('fr')).toBe('ru');
    expect(resolveLocaleFromCookie('----')).toBe('ru');
  });
});
