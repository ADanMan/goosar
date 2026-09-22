import { describe, expect, it } from 'vitest';
import { BLOCKED_IMAGE_ACCESSIBILITY_LABEL } from './blocked-image-label';

describe('blocked-image accessibility label', () => {
  it('is the canonical English fallback announced to VoiceOver', () => {
    expect(BLOCKED_IMAGE_ACCESSIBILITY_LABEL).toBe('Blocked external image');
  });

  it('is a non-empty human string, not an enum/key', () => {
    expect(BLOCKED_IMAGE_ACCESSIBILITY_LABEL.trim()).not.toBe('');
    expect(BLOCKED_IMAGE_ACCESSIBILITY_LABEL).not.toMatch(/^[a-z0-9_.]+$/);
  });
});
