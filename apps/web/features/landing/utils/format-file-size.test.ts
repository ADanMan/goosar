import { describe, expect, it } from 'vitest';
import { formatFileSize } from './format-file-size';

const UNITS = { b: 'Б', kb: 'КБ', mb: 'МБ', gb: 'ГБ' };

describe('formatFileSize', () => {
  it('formats bytes below 1024 with no decimals', () => {
    expect(formatFileSize(512, UNITS)).toBe('512 Б');
  });

  it('formats megabytes with one decimal below 10', () => {
    expect(formatFileSize(8.4 * 1024 * 1024, UNITS)).toBe('8.4 МБ');
  });

  it('rounds megabytes with no decimals at or above 10', () => {
    expect(formatFileSize(128 * 1024 * 1024, UNITS)).toBe('128 МБ');
  });

  it('formats gigabytes', () => {
    expect(formatFileSize(1.5 * 1024 * 1024 * 1024, UNITS)).toBe('1.5 ГБ');
  });

  it('returns null for zero, negative or non-finite input', () => {
    expect(formatFileSize(0, UNITS)).toBeNull();
    expect(formatFileSize(-10, UNITS)).toBeNull();
    expect(formatFileSize(Number.NaN, UNITS)).toBeNull();
  });
});
