import { afterEach, describe, expect, it, vi } from 'vitest';

import { runTaskPreflight, setTaskPreflight } from './task-preflight';

afterEach(() => setTaskPreflight(null));

describe('task preflight (#466)', () => {
  it('allows work when no platform registered a check', async () => {
    await expect(runTaskPreflight()).resolves.toBe(true);
  });

  it('stops work when the registered check says no', async () => {
    setTaskPreflight(async () => false);
    await expect(runTaskPreflight()).resolves.toBe(false);
  });

  it('lets work through when the check says yes', async () => {
    setTaskPreflight(async () => true);
    await expect(runTaskPreflight()).resolves.toBe(true);
  });

  it('allows work when the check throws — a broken probe must not block a send', async () => {
    setTaskPreflight(async () => {
      throw new Error('IPC gone');
    });
    await expect(runTaskPreflight()).resolves.toBe(true);
  });

  it('replaces rather than accumulates, and clears on null', async () => {
    const first = vi.fn(async () => false);
    setTaskPreflight(first);
    setTaskPreflight(async () => true);
    await expect(runTaskPreflight()).resolves.toBe(true);
    expect(first).not.toHaveBeenCalled();

    setTaskPreflight(null);
    await expect(runTaskPreflight()).resolves.toBe(true);
  });
});
