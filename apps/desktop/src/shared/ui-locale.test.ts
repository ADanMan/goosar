import { describe, expect, it } from 'vitest';
import { DEFAULT_LOCALE, SUPPORTED_LOCALES } from '@goosar/core/i18n';
import { DEFAULT_DESKTOP_UI_LOCALE, DESKTOP_UI_LOCALES, parseUiLocale } from './ui-locale';

describe('desktop UI locale copy of the core locale list', () => {
  it('ships exactly the locales core supports', () => {
    expect([...DESKTOP_UI_LOCALES].sort()).toEqual([...SUPPORTED_LOCALES].sort());
  });

  it('defaults to the same locale core does', () => {
    expect(DEFAULT_DESKTOP_UI_LOCALE).toBe(DEFAULT_LOCALE);
  });
});

describe('parseUiLocale', () => {
  it('accepts every shipped locale', () => {
    for (const locale of DESKTOP_UI_LOCALES) {
      expect(parseUiLocale(locale)).toBe(locale);
    }
  });

  it('rejects anything this build has no copy for', () => {
    for (const value of ['fr', 'ru-RU', 'EN', '', '   ', null, 42, {}]) {
      expect(parseUiLocale(value)).toBe(null);
    }
  });
});
