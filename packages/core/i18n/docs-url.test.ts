import { describe, expect, it } from 'vitest';
import { docsLocalePath, docsUrl } from './docs-url';

describe('docsLocalePath', () => {
  it('returns the prefix-free root for Russian, the default docs locale', () => {
    expect(docsLocalePath('ru')).toBe('/docs');
    expect(docsLocalePath('ru-RU')).toBe('/docs');
  });

  it('prefixes English, the only other locale the docs site builds', () => {
    expect(docsLocalePath('en')).toBe('/docs/en');
    expect(docsLocalePath('en-US')).toBe('/docs/en');
  });

  it('falls back to the default docs for unknown or missing languages', () => {
    expect(docsLocalePath('de')).toBe('/docs');
    expect(docsLocalePath(undefined)).toBe('/docs');
    expect(docsLocalePath(null)).toBe('/docs');
    expect(docsLocalePath('')).toBe('/docs');
  });
});

describe('docsUrl', () => {
  it('keeps the page path and anchor across locales', () => {
    expect(docsUrl('ru', '/install-agent-runtime#troubleshooting')).toBe(
      'https://goosar.ru/docs/install-agent-runtime#troubleshooting',
    );
    expect(docsUrl('en', '/install-agent-runtime#troubleshooting')).toBe(
      'https://goosar.ru/docs/en/install-agent-runtime#troubleshooting',
    );
  });

  it('returns the docs root when no path is given', () => {
    expect(docsUrl('ru')).toBe('https://goosar.ru/docs');
    expect(docsUrl('en')).toBe('https://goosar.ru/docs/en');
  });
});
