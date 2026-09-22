import { describe, expect, it } from 'vitest';
import { normalizeServerUrl } from './server-url';

describe('normalizeServerUrl', () => {
  it('upgrades a plain http address to https', () => {
    expect(normalizeServerUrl('http://goosar.ru')).toBe('https://goosar.ru');
  });

  it('prepends https to a bare host with no scheme', () => {
    expect(normalizeServerUrl('goosar.ru')).toBe('https://goosar.ru');
  });

  it('lowercases the scheme and host and trims a trailing slash', () => {
    expect(normalizeServerUrl('HTTP://Goosar.RU/')).toBe('https://goosar.ru');
  });

  it('leaves an http localhost address unchanged', () => {
    expect(normalizeServerUrl('http://localhost:8080')).toBe('http://localhost:8080');
  });

  it('leaves an http 127.0.0.1 address unchanged', () => {
    expect(normalizeServerUrl('http://127.0.0.1:3000')).toBe('http://127.0.0.1:3000');
  });

  it('leaves any 127.0.0.0/8 loopback address unchanged', () => {
    expect(normalizeServerUrl('http://127.5.0.1:9000')).toBe('http://127.5.0.1:9000');
  });

  it('leaves an http IPv6 loopback address unchanged', () => {
    expect(normalizeServerUrl('http://[::1]:8080')).toBe('http://[::1]:8080');
  });

  it('leaves an http 192.168.x.x address unchanged', () => {
    expect(normalizeServerUrl('http://192.168.1.5')).toBe('http://192.168.1.5');
  });

  it('leaves an http 10.x.x.x address unchanged', () => {
    expect(normalizeServerUrl('http://10.0.0.5:8080')).toBe('http://10.0.0.5:8080');
  });

  it('leaves an http 172.16-31.x.x address unchanged', () => {
    expect(normalizeServerUrl('http://172.20.0.5')).toBe('http://172.20.0.5');
  });

  it('upgrades an http 172.x.x.x address outside the 172.16-31 private range', () => {
    expect(normalizeServerUrl('http://172.32.0.5')).toBe('https://172.32.0.5');
  });

  it('leaves an https address unchanged', () => {
    expect(normalizeServerUrl('https://x.example')).toBe('https://x.example');
  });

  it('trims a trailing slash from an https address', () => {
    expect(normalizeServerUrl('https://x.example/')).toBe('https://x.example');
  });

  it('trims surrounding whitespace before normalizing', () => {
    expect(normalizeServerUrl('  http://goosar.ru  ')).toBe('https://goosar.ru');
  });

  it('returns an empty string unchanged', () => {
    expect(normalizeServerUrl('')).toBe('');
  });

  it('returns whitespace-only input trimmed to an empty string', () => {
    expect(normalizeServerUrl('   ')).toBe('');
  });

  it('hands back unparseable garbage unchanged (trimmed) for the caller to validate', () => {
    expect(normalizeServerUrl('not a url')).toBe('not a url');
  });

  it('preserves a non-root path and query on the normalized address', () => {
    expect(normalizeServerUrl('http://goosar.ru/api?x=1')).toBe('https://goosar.ru/api?x=1');
  });
});
