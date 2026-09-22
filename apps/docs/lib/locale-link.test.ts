import { describe, expect, it } from 'vitest';
import { prefixLocale } from './locale-link';

describe('prefixLocale', () => {
  it('prefixes root-relative paths with the active non-default locale', () => {
    expect(prefixLocale('/developers/architecture', 'en')).toBe('/en/developers/architecture');
    expect(prefixLocale('/security', 'en')).toBe('/en/security');
  });

  it('preserves anchors and query strings on prefixed paths', () => {
    expect(prefixLocale('/install-agent-runtime#troubleshooting', 'en')).toBe(
      '/en/install-agent-runtime#troubleshooting',
    );
    expect(prefixLocale('/security?from=docs', 'en')).toBe('/en/security?from=docs');
  });

  it('maps the docs root to the bare locale root', () => {
    expect(prefixLocale('/', 'en')).toBe('/en');
  });

  it('leaves links alone in the default language', () => {
    expect(prefixLocale('/security', 'ru')).toBe('/security');
    expect(prefixLocale('/', 'ru')).toBe('/');
  });

  it('does not double-prefix a path that already carries a locale', () => {
    expect(prefixLocale('/en/security', 'en')).toBe('/en/security');
    expect(prefixLocale('/ru/security', 'en')).toBe('/ru/security');
  });

  it('does not touch external links', () => {
    expect(prefixLocale('https://goosar.ru/download', 'en')).toBe('https://goosar.ru/download');
    expect(prefixLocale('mailto:hello@goosar.ru', 'en')).toBe('mailto:hello@goosar.ru');
    expect(prefixLocale('tel:+1234567890', 'en')).toBe('tel:+1234567890');
  });

  it('does not touch in-page anchors or relative paths', () => {
    expect(prefixLocale('#section', 'en')).toBe('#section');
    expect(prefixLocale('./sibling', 'en')).toBe('./sibling');
    expect(prefixLocale('../sibling', 'en')).toBe('../sibling');
  });

  it('returns an empty href unchanged', () => {
    expect(prefixLocale('', 'en')).toBe('');
  });
});
