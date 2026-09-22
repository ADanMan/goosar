import { describe, expect, it } from 'vitest';
import { workspaceUrlHost } from './workspace-url';

describe('workspaceUrlHost', () => {
  it('returns the host of a full app URL', () => {
    expect(workspaceUrlHost('https://goosar.example.com')).toBe('goosar.example.com');
  });

  it('ignores scheme, path, and trailing slash', () => {
    expect(workspaceUrlHost('https://goosar.example.com/')).toBe('goosar.example.com');
    expect(workspaceUrlHost('http://goosar.example.com/app/onboarding')).toBe('goosar.example.com');
  });

  it('preserves a non-default port', () => {
    expect(workspaceUrlHost('https://my.host:3000')).toBe('my.host:3000');
  });

  it('accepts a bare host without a scheme', () => {
    expect(workspaceUrlHost('goosar.example.com')).toBe('goosar.example.com');
    expect(workspaceUrlHost('goosar.example.com/path')).toBe('goosar.example.com');
  });

  it('falls back to the brand host when no app URL is configured', () => {
    expect(workspaceUrlHost('')).toBe('goosar.ru');
    expect(workspaceUrlHost('   ')).toBe('goosar.ru');
    expect(workspaceUrlHost(null)).toBe('goosar.ru');
    expect(workspaceUrlHost(undefined)).toBe('goosar.ru');
  });
});
