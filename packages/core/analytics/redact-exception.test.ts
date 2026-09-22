import { describe, expect, it } from 'vitest';
import { redactText, redactExceptionProperties } from './redact-exception';

describe('redactText', () => {
  it('redacts email addresses', () => {
    expect(redactText('Invalid email: alice@example.com')).toBe('Invalid email: [redacted]');
  });

  it('strips URL query strings that may carry tokens, keeping host + path', () => {
    expect(redactText('fetch failed https://goosar.ru/issues?token=abc123secret')).toBe(
      'fetch failed https://goosar.ru/issues?[redacted]',
    );
  });

  it('redacts long opaque tokens (JWT / API key / uuid)', () => {
    expect(redactText('auth header eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9')).toBe(
      'auth header [redacted]',
    );
  });

  it('keeps the non-sensitive part of a message intact', () => {
    expect(redactText("Cannot read property 'x' of undefined")).toBe(
      "Cannot read property 'x' of undefined",
    );
  });

  it('passes through non-strings unchanged', () => {
    expect(redactText(undefined)).toBeUndefined();
    expect(redactText(42)).toBe(42);
  });

  it('redacts short bearer tokens the long-token rule misses', () => {
    expect(redactText('401 with Bearer abc123')).toBe('401 with Bearer [redacted]');
    expect(redactText('auth: Basic dXNlcjpwYXNz failed')).toBe('auth: Basic [redacted] failed');
  });

  it('redacts short non-Bearer secret assignments', () => {
    expect(redactText('rejected X-API-Key: sk-live-abc123')).toBe('rejected X-API-Key: [redacted]');
    expect(redactText('bad request apikey=short-key-12')).toBe('bad request apikey=[redacted]');
    expect(redactText('login failed password=hunter2')).toBe('login failed password=[redacted]');
    expect(redactText('config error: secret = oops')).toBe('config error: secret = [redacted]');
    expect(redactText('the cancellation token was disposed')).toBe(
      'the cancellation token was disposed',
    );
  });

  it('redacts the username segment of home-directory paths', () => {
    expect(redactText('ENOENT /Users/alice/projects/app/config.json')).toBe(
      'ENOENT /Users/[redacted]/projects/app/config.json',
    );
    expect(redactText('read /home/bob/.ssh/known_hosts')).toBe(
      'read /home/[redacted]/.ssh/known_hosts',
    );
    expect(redactText('open C:\\Users\\carol\\file.txt')).toBe(
      'open C:\\Users\\[redacted]\\file.txt',
    );
  });

  it('redacts IPv4 addresses', () => {
    expect(redactText('connect ECONNREFUSED 192.168.1.42:8081')).toBe(
      'connect ECONNREFUSED [redacted]:8081',
    );
  });

  it('redacts long quoted free text but keeps short quoted identifiers', () => {
    const typed = 'a'.repeat(60);
    expect(redactText(`Unexpected token in "${typed}"`)).toBe('Unexpected token in "[redacted]"');
    expect(redactText("Cannot read property 'x' of undefined")).toBe(
      "Cannot read property 'x' of undefined",
    );
  });

  it('truncates oversized messages after redaction', () => {
    const out = redactText(`prefix ${'z '.repeat(600)}`) as string;
    expect(out.endsWith('… [truncated]')).toBe(true);
    expect(out.length).toBeLessThan(600);
  });
});

describe('redactExceptionProperties', () => {
  it('scrubs the message and each $exception_list value, leaving frames untouched', () => {
    const props = {
      $exception_message: 'Bad email bob@corp.com',
      $exception_list: [
        {
          type: 'TypeError',
          value: 'Token leaked: ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
          stacktrace: { frames: [{ filename: 'app.tsx', lineno: 5, function: 'render' }] },
        },
      ],
    };

    redactExceptionProperties(props);

    const entry = props.$exception_list[0]!;
    expect(props.$exception_message).toBe('Bad email [redacted]');
    expect(entry.value).toBe('Token leaked: [redacted]');
    expect(entry.stacktrace.frames[0]).toEqual({
      filename: 'app.tsx',
      lineno: 5,
      function: 'render',
    });
    expect(entry.type).toBe('TypeError');
  });

  it('is safe on undefined / malformed properties', () => {
    expect(redactExceptionProperties(undefined)).toBeUndefined();
    expect(() =>
      redactExceptionProperties({ $exception_list: 'not-an-array' as unknown as [] }),
    ).not.toThrow();
  });
});
