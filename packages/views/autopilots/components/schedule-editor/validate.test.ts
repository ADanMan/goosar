import { describe, it, expect, vi } from 'vitest';
import { QueryClient } from '@tanstack/react-query';
import { ApiError } from '@goosar/core/api';
import { findScheduleRejection } from './validate';
import { parseCron } from './cron-mapping';

const behavior: { mode: 'ok' | 'invalid_cron' | 'invalid_timezone' | 'transport' | 'no_code' } = {
  mode: 'ok',
};

vi.mock('@goosar/core/autopilots/queries', () => ({
  cronPreviewOptions: (wsId: string, expr: string, tz: string) => ({
    queryKey: ['autopilots', wsId, 'cron-preview', expr, tz, behavior.mode],
    queryFn: async () => {
      if (behavior.mode === 'transport') {
        throw new ApiError('API error: 502 Bad Gateway', 502, 'Bad Gateway');
      }
      if (behavior.mode === 'invalid_cron') {
        throw new ApiError('parse cron: expected exactly 5 fields', 400, 'Bad Request', {
          error: 'parse cron: expected exactly 5 fields',
          code: 'invalid_cron',
        });
      }
      if (behavior.mode === 'invalid_timezone') {
        throw new ApiError(`invalid timezone "${tz}"`, 400, 'Bad Request', {
          error: `invalid timezone "${tz}"`,
          code: 'invalid_timezone',
        });
      }
      if (behavior.mode === 'no_code') {
        throw new ApiError('bad request', 400, 'Bad Request', 'bad request');
      }
      return { next_runs: ['2126-07-14T01:00:00Z'] };
    },
    retry: false,
  }),
}));

function run() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return findScheduleRejection(qc, 'ws-test', parseCron('0 9 * * *', 'UTC'));
}

describe('findScheduleRejection', () => {
  it('returns null when the server accepts the schedule', async () => {
    behavior.mode = 'ok';
    await expect(run()).resolves.toBeNull();
  });

  it("returns the cron rejection with the parser's own words", async () => {
    behavior.mode = 'invalid_cron';
    await expect(run()).resolves.toEqual({
      code: 'invalid_cron',
      detail: 'parse cron: expected exactly 5 fields',
    });
  });

  it('blames the timezone when the server rejects the zone', async () => {
    behavior.mode = 'invalid_timezone';
    const rejection = await run();
    expect(rejection?.code).toBe('invalid_timezone');
  });

  it('falls back to invalid_cron when a 400 carries no readable code', async () => {
    behavior.mode = 'no_code';
    const rejection = await run();
    expect(rejection?.code).toBe('invalid_cron');
  });

  it('does not block the save on a transport or server failure', async () => {
    behavior.mode = 'transport';
    await expect(run()).resolves.toBeNull();
  });
});
