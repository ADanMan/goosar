import { describe, expect, it } from 'vitest';
import { customRuntimeDocsHref, installRuntimeDocsHref } from './runtime-docs';

describe('runtime docs links', () => {
  it.each([
    ['ru', 'https://goosar.ru/docs/install-agent-runtime'],
    ['en', 'https://goosar.ru/docs/en/install-agent-runtime'],
    ['zh-Hans', 'https://goosar.ru/docs/install-agent-runtime'],
  ])('localizes the install guide for %s', (language, expected) => {
    expect(installRuntimeDocsHref(language)).toBe(expected);
  });

  it('links the custom runtimes page', () => {
    expect(customRuntimeDocsHref('ru')).toBe('https://goosar.ru/docs/custom-runtimes');
    expect(customRuntimeDocsHref('en')).toBe('https://goosar.ru/docs/en/custom-runtimes');
  });
});
