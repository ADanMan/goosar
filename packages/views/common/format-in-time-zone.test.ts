import { describe, expect, it } from 'vitest';
import { formatInTimeZone } from './format-in-time-zone';

describe('formatInTimeZone', () => {
  const iso = '2026-07-14T01:00:00Z';

  it('renders the instant in the given timezone', () => {
    const out = formatInTimeZone(iso, 'America/Los_Angeles', 'en-US');
    expect(out).toContain('13');
    expect(out).toMatch(/6:00\s?PM|18:00/);
  });

  it('renders the same instant differently in another timezone', () => {
    const out = formatInTimeZone(iso, 'Asia/Shanghai', 'en-US');
    expect(out).toContain('14');
    expect(out).toMatch(/9:00\s?AM|09:00/);
  });

  it('falls back to local time for a zone this runtime does not know', () => {
    expect(formatInTimeZone(iso, 'Not/AZone', 'en-US')).not.toBe('');
    expect(formatInTimeZone(iso, 'Not/AZone', 'en-US')).not.toBe(iso);
  });

  it("keeps the reader's locale when it falls back over a bad zone", () => {
    expect(formatInTimeZone(iso, 'Not/AZone', 'zh-CN')).toMatch(/月/);
  });

  it('hands back an unreadable timestamp instead of throwing', () => {
    expect(formatInTimeZone('not-a-date', 'UTC', 'en-US')).toBe('not-a-date');
  });
});
