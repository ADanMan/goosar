import { describe, expect, it } from 'vitest';
import { bucketFor, inPercent } from './hash';

describe('feature-flags hash', () => {
  it('bucketFor returns a value in [0, 100)', () => {
    for (const id of ['a', 'b', 'user-1', 'user-2', '', '🦄']) {
      const b = bucketFor('flag', id);
      expect(b).toBeGreaterThanOrEqual(0);
      expect(b).toBeLessThan(100);
    }
  });

  it('bucketFor is deterministic for the same (key, id)', () => {
    const first = bucketFor('billing_new_invoice', 'user-42');
    for (let i = 0; i < 100; i++) {
      expect(bucketFor('billing_new_invoice', 'user-42')).toBe(first);
    }
  });

  it('separator prevents key/id boundary collisions', () => {
    expect(bucketFor('ab', 'c')).not.toBe(bucketFor('a', 'bc'));
  });

  it('cross-language golden: bucket values match the Go side exactly', () => {
    const cases: ReadonlyArray<[string, string, number]> = [
      ['billing_new_invoice', 'user-42', 97],
      ['feature_a', 'user-1', 50],
      ['checkout_algo', 'u-7f8a', 11],
      ['ws_rollout', 'workspace-1', 62],
      ['empty_id_flag', '', 83],
      ['flag', 'é', 53],
      ['flag', '🦄', 82],
      ['实验', 'user-1', 90],
      ['flag', '用户-1', 95],
      ['checkout_算法', 'user-100', 79],
    ];
    for (const [key, id, want] of cases) {
      expect(bucketFor(key, id)).toBe(want);
    }
  });

  it('inPercent clamps boundary values', () => {
    expect(inPercent('any', 'any', 0)).toBe(false);
    expect(inPercent('any', 'any', -10)).toBe(false);
    expect(inPercent('any', 'any', 100)).toBe(true);
    expect(inPercent('any', 'any', 999)).toBe(true);
  });

  it('inPercent splits a 50% rollout roughly in half across 1000 users', () => {
    let enabled = 0;
    for (let i = 0; i < 1000; i++) {
      if (inPercent('split', `user-${i.toString(36)}`, 50)) enabled++;
    }
    expect(enabled).toBeGreaterThan(400);
    expect(enabled).toBeLessThan(600);
  });
});
