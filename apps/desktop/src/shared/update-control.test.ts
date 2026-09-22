import { describe, expect, it } from 'vitest';
import { parseUpdateControl } from './update-control';

describe('parseUpdateControl', () => {
  it('fails toward cloud for absent or malformed payloads', () => {
    expect(parseUpdateControl(undefined)).toEqual({ perimeterProfile: false });
    expect(parseUpdateControl(null)).toEqual({ perimeterProfile: false });
    expect(parseUpdateControl('perimeter')).toEqual({
      perimeterProfile: false,
    });
    expect(parseUpdateControl({})).toEqual({ perimeterProfile: false });
    expect(parseUpdateControl({ perimeterProfile: 'true' })).toEqual({
      perimeterProfile: false,
    });
    expect(parseUpdateControl({ perimeterProfile: 1 })).toEqual({
      perimeterProfile: false,
    });
  });

  it('recognizes only an explicit true', () => {
    expect(parseUpdateControl({ perimeterProfile: true })).toEqual({
      perimeterProfile: true,
    });
  });
});
