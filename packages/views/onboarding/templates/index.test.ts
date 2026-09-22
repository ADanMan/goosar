import { describe, expect, it } from 'vitest';
import { pickContentLang } from './index';

describe('pickContentLang', () => {
  it('uses the shared locale matcher before selecting persisted content', () => {
    expect(pickContentLang('en-US')).toBe('en');
    expect(pickContentLang('zh-Hant')).toBe('zh');
    expect(pickContentLang('ko-KR')).toBe('ko');
    expect(pickContentLang('ja-JP')).toBe('ja');
  });

  it('treats Russian as a first-class persisted-content language', () => {
    expect(pickContentLang('ru')).toBe('ru');
    expect(pickContentLang('ru-RU')).toBe('ru');
  });

  it('falls back to the product default (Russian) for unsupported or missing languages', () => {
    expect(pickContentLang('fr-FR')).toBe('ru');
    expect(pickContentLang(null)).toBe('ru');
    expect(pickContentLang(undefined)).toBe('ru');
  });
});
