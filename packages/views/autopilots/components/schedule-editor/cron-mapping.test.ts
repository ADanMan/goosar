import { describe, it, expect } from 'vitest';
import { cronFields, parseCron, toCron } from './cron-mapping';
import type { ScheduleConfig } from './model';
import { getDefaultScheduleConfig } from './model';

const TZ = 'Asia/Shanghai';

function structured(expr: string): ScheduleConfig {
  const parsed = parseCron(expr, TZ);
  expect(parsed.raw, `expected ${JSON.stringify(expr)} to be structurable`).toBeNull();
  return parsed;
}

describe('parseCron — structurable expressions', () => {
  it('parses a fixed daily time', () => {
    const p = structured('0 9 * * *');
    expect(p.time).toEqual({ kind: 'at', time: '09:00' });
    expect(p.days).toEqual({ kind: 'every' });
    expect(p.timezone).toBe(TZ);
  });

  it('parses zero-padded fields', () => {
    expect(structured('05 09 * * *').time).toEqual({ kind: 'at', time: '09:05' });
  });

  it('parses the weekday preset shape', () => {
    const p = structured('30 18 * * 1-5');
    expect(p.time).toEqual({ kind: 'at', time: '18:30' });
    expect(p.days).toEqual({ kind: 'weekly', daysOfWeek: [1, 2, 3, 4, 5] });
  });

  it('tolerates surrounding and repeated whitespace', () => {
    expect(structured('  0  9  *  *  *  ').time).toEqual({ kind: 'at', time: '09:00' });
  });

  it.each([
    ['minute', '0, 9 * * *'],
    ['leading comma in minute', ',0 9 * * *'],
    ['hour', '0 9, * * *'],
    ['leading comma in hour', '0 ,9 * * *'],
  ])('reads through a stray comma in the %s field', (_label, expr) => {
    expect(structured(expr).time).toEqual({ kind: 'at', time: '09:00' });
  });

  it('reads through a stray comma in the day-of-month field', () => {
    expect(structured('0 9 15, * *').days).toEqual({ kind: 'monthly', dayOfMonth: 15 });
  });

  it.each([
    ['minute', '? 10 * * *'],
    ['hour', '0 ? * * *'],
    ['day-of-month', '0 9 ? * 1-5'],
    ['day-of-week', '0 9 15 * ?'],
  ])('takes a question mark in the %s field as a wildcard', (_label, expr) => {
    expect(structured(expr).raw).toBeNull();
  });
  it('reads a question mark as the wildcard it is', () => {
    const p = structured('? 10 * * *');
    expect(p.time).toEqual({
      kind: 'every',
      unit: 'minutes',
      interval: 1,
      minute: 0,
      window: { from: '10:00', to: '10:59' },
    });
    expect(cronFields(p)).toBe('* 10 * * *');
  });

  it.each([
    ['an upper end on a star', '0 *-19 * * *', '0 * * * *'],
    ['an upper end on a question mark', '0 ?-19 * * *', '0 * * * *'],
    ['a stepped wildcard range', '0 *-19/2 * * *', '0 */2 * * *'],
    ['the same in the minute field', '*-30/10 9 * * *', '*/10 9 * * *'],
    ['nonsense past the dash, which is never read', '0 *-abc * * *', '0 * * * *'],
    ['more dashes than a range may have', '0 *-10-20 * * *', '0 * * * *'],
    ['day-of-week', '0 9 * * ?-5', '0 9 * * *'],
    ['day-of-month', '0 9 *-15 * *', '0 9 * * *'],
  ])(
    "reads a wildcard's unread upper end the way the server does: %s",
    (_label, expr, canonical) => {
      const p = structured(expr);
      expect(cronFields(p)).toBe(canonical);
    },
  );

  it("leaves a question mark that is not a range's low end alone", () => {
    expect(parseCron('0 9-? * * *', TZ).raw).toBe('0 9-? * * *');
  });

  it.each([
    ['minute', '0,* 9 * * *', '* 9 * * *'],
    ['minute, beside a stepped wildcard', '*,*/2 9 * * *', '* 9 * * *'],
    ['minute, spelled with a question mark', '?,15 9 * * *', '* 9 * * *'],
    ['minute, with an unread upper end', '0,*-30 9 * * *', '* 9 * * *'],
    ['hour', '0 9,* * * *', '0 * * * *'],
    ['day-of-month', '0 9 1,* * *', '0 9 * * *'],
    ['month', '0 9 * 1,* *', '0 9 * * *'],
    ['month, with the wildcard beside a name', '0 9 * JAN,* *', '0 9 * * *'],
    ['day-of-week', '0 9 * * 1,*', '0 9 * * *'],
  ])('collapses a list carrying a wildcard in the %s field', (_label, expr, canonical) => {
    expect(cronFields(structured(expr))).toBe(canonical);
  });

  it.each([['*,abc 9 * * *'], ['*,60 9 * * *'], ['0 9 *,32 * *'], ['0 9 * FOO,* *']])(
    'leaves a wildcard list with an unparseable part alone: %s',
    (expr) => {
      expect(parseCron(expr, TZ).raw).toBe(expr);
    },
  );

  it.each([
    ['step wider than the minute field', '*/65 * * * *', '0 * * * *'],
    ['step wider than the hour field', '0 */24 * * *', '0 0 * * *'],
    ['step wider than its hour range', '0 10-20/30 * * *', '0 10 * * *'],
    ['step wider than the dom field', '0 9 */40 * *', '0 9 1 * *'],
    ['range of one value', '5-5 9 * * *', '5 9 * * *'],
    ['three-digit step', '*/100 * * * *', '0 * * * *'],
  ])('collapses a degenerate %s', (_label, expr, canonical) => {
    const p = structured(expr);
    expect(p.raw).toBeNull();
    expect(cronFields(p)).toBe(canonical);
  });

  it('keeps an hour window whose step outruns it', () => {
    const p = structured('0 9-19/23 * * *');
    expect(p.time).toEqual({
      kind: 'every',
      unit: 'hours',
      interval: 23,
      minute: 0,
      window: { from: '09:00', to: '19:00' },
    });
    expect(cronFields(p)).toBe('0 9-19/23 * * *');
  });

  it('structures a degenerate step across all three dimensions', () => {
    const p = structured('*/65 10-20 14 * *');
    expect(p.days).toEqual({ kind: 'monthly', dayOfMonth: 14 });
    expect(p.time).toEqual({
      kind: 'every',
      unit: 'hours',
      interval: 1,
      minute: 0,
      window: { from: '10:00', to: '20:00' },
    });
    expect(cronFields(p)).toBe('0 10-20 14 * *');
  });

  it.each([
    ['0 * * * *', 1, 0],
    ['15 * * * *', 1, 15],
    ['0 */2 * * *', 2, 0],
    ['15 */3 * * *', 3, 15],
  ])('parses hourly-interval %s', (expr, interval, minute) => {
    expect(structured(expr).time).toEqual({
      kind: 'every',
      unit: 'hours',
      interval,
      minute,
      window: null,
    });
  });

  it.each([
    ['0 0-23 * * *', 'hours', 1, 0],
    ['0 0-23/3 * * *', 'hours', 3, 0],
    ['15 0-23/2 * * *', 'hours', 2, 15],
    ['*/30 0-23 * * *', 'minutes', 30, 0],
    ['*/30 0-23/1 * * *', 'minutes', 30, 0],
  ])('reads an hour range that spans the day as all day: %s', (expr, unit, interval, minute) => {
    expect(structured(expr).time).toEqual({
      kind: 'every',
      unit,
      interval,
      minute,
      window: null,
    });
  });

  it.each([
    ['0 9-21 * * *', 1, '09:00', '21:00', 0],
    ['30 9-21 * * *', 1, '09:30', '21:30', 30],
    ['0 9-21/2 * * *', 2, '09:00', '21:00', 0],
  ])('parses hour window %s', (expr, interval, from, to, minute) => {
    expect(structured(expr).time).toEqual({
      kind: 'every',
      unit: 'hours',
      interval,
      minute,
      window: { from, to },
    });
  });

  it.each([
    ['0 9/2 * * *', 2, '09:00', '23:00', 0, '0 9-23/2 * * *'],
    ['30 9/1 * * *', 1, '09:30', '23:30', 30, '30 9-23 * * *'],
    ['0 0/2 * * *', 2, null, null, 0, '0 */2 * * *'],
    ['0 ?/2 * * *', 2, null, null, 0, '0 */2 * * *'],
  ])(
    'reads a bare hour with a step as running to the field max: %s',
    (expr, interval, from, to, minute, back) => {
      const p = structured(expr);
      expect(p.time).toEqual({
        kind: 'every',
        unit: 'hours',
        interval,
        minute,
        window: from === null ? null : { from, to },
      });
      expect(cronFields(p)).toBe(back);
    },
  );

  it.each([
    ['0 9-9 * * *', 1, '09:00', '09:00'],
    ['0 9-9/3 * * *', 3, '09:00', '09:00'],
    ['0 9-11/5 * * *', 5, '09:00', '11:00'],
    ['0 9-21/23 * * *', 23, '09:00', '21:00'],
  ])('keeps a degenerate hour window as a window: %s', (expr, interval, from, to) => {
    expect(structured(expr).time).toEqual({
      kind: 'every',
      unit: 'hours',
      interval,
      minute: 0,
      window: { from, to },
    });
  });

  it.each([
    ['*/5 9-9/3 * * *', 5, '09:00', '09:59'],
    ['*/10 9-21/23 * * *', 10, '09:00', '09:59'],
    ['*/10 9-9 * * *', 10, '09:00', '09:59'],
  ])(
    'reads a degenerate stepped hour beside a minute step as a single-hour window: %s',
    (expr, interval, from, to) => {
      expect(structured(expr).time).toEqual({
        kind: 'every',
        unit: 'minutes',
        interval,
        minute: 0,
        window: { from, to },
      });
    },
  );

  it.each([
    ['0 9-9/24 * * *', '09:00'],
    ['0 9-21/30 * * *', '09:00'],
    ['0 */24 * * *', '00:00'],
  ])("collapses an hour step past the model's bound to the value it selects: %s", (expr, time) => {
    expect(structured(expr).time).toEqual({ kind: 'at', time });
  });

  it.each([
    ['0 9 * * */100', [0]],
    ['0 9 * * 1-5/60', [1]],
    ['0 9 * * MON-FRI/60', [1]],
    ['0 9 * * */7', [0]],
  ])('reads a day-of-week step wider than the week: %s', (expr, daysOfWeek) => {
    expect(structured(expr).days).toEqual({ kind: 'weekly', daysOfWeek });
  });

  it('parses a compound expression across all three dimensions', () => {
    const p = structured('0 9-21/2 * * 2-4');
    expect(p.time).toEqual({
      kind: 'every',
      unit: 'hours',
      interval: 2,
      minute: 0,
      window: { from: '09:00', to: '21:00' },
    });
    expect(p.days).toEqual({ kind: 'weekly', daysOfWeek: [2, 3, 4] });
  });

  it.each([
    ['* * * * *', 1, null],
    ['*/10 * * * *', 10, null],
    ['0/5 * * * *', 5, null],
    ['*/10 9-18 * * *', 10, { from: '09:00', to: '18:59' }],
    ['* 9 * * *', 1, { from: '09:00', to: '09:59' }],
    ['*/15 9 * * *', 15, { from: '09:00', to: '09:59' }],
  ])('parses minute-interval %s', (expr, interval, window) => {
    expect(structured(expr).time).toEqual({
      kind: 'every',
      unit: 'minutes',
      interval,
      minute: 0,
      window,
    });
  });

  it.each([
    ['30 10 15 * *', { kind: 'at', time: '10:30' }, 15],
    ['0 9 1 * *', { kind: 'at', time: '09:00' }, 1],
    ['0 9 31 * *', { kind: 'at', time: '09:00' }, 31],
    [
      '*/10 * 15 * *',
      { kind: 'every', unit: 'minutes', interval: 10, minute: 0, window: null },
      15,
    ],
  ])('parses monthly %s', (expr, time, dayOfMonth) => {
    const p = structured(expr);
    expect(p.time).toEqual(time);
    expect(p.days).toEqual({ kind: 'monthly', dayOfMonth });
  });

  it.each([
    ['0 9 * * 0', [0]],
    ['0 9 * * 6', [6]],
    ['0 9 * * 1,3,5', [1, 3, 5]],
    ['0 9 * * 5,1,5', [1, 5]],
    ['0 9 * * 0-6', [0, 1, 2, 3, 4, 5, 6]],
    ['0 9 * * MON', [1]],
    ['0 9 * * SUN', [0]],
    ['0 9 * * SAT', [6]],
    ['0 9 * * mon-fri', [1, 2, 3, 4, 5]],
    ['0 9 * * MON,WED', [1, 3]],
    ['0 9 * * */2', [0, 2, 4, 6]],
    ['0 9 * * 1-5/2', [1, 3, 5]],
    ['0 9 * * 1/2', [1, 3, 5]],
    ['0 9 * * MON/2', [1, 3, 5]],
    ['0 9 * * */7', [0]],
    ['0 9 * * 1,3-5', [1, 3, 4, 5]],
    ['0 9 * * 1-5,', [1, 2, 3, 4, 5]],
    ['0 9 * * 1,,5', [1, 5]],
    ['0 9 * * ,1,5', [1, 5]],
  ])('expands dow %s to chips %j', (expr, days) => {
    expect(structured(expr).days).toEqual({ kind: 'weekly', daysOfWeek: days });
  });
});

describe('parseCron — advanced-only fallback', () => {
  it.each([
    ['minute range', '0-30 9 * * *'],
    ['minute list', '0,30 9 * * *'],
    ['minute anchored step', '15/5 * * * *'],
    ['minute out of range', '60 9 * * *'],
    ['negative minute', '-5 9 * * *'],
    ['overflowing minute', '99999999999999999999 9 * * *'],
    ['hour list', '0 9,12,15 * * *'],
    ['hour out of range', '0 24 * * *'],
    ['hour wraparound range', '0 21-9 * * *'],
    ['minute step with hour step', '*/10 */2 * * *'],
    ['every minute with hour step', '* */2 * * *'],
    ['minute step with stepped window', '*/10 9-18/2 * * *'],
    ['question-mark steps in both time fields', '?/2 ?/2 * * ?/2'],
    ['dom list', '0 9 1,15 * *'],
    ['dom range', '0 9 1-15 * *'],
    ['dom step', '0 9 */2 * *'],
    ['dom zero', '0 9 0 * *'],
    ['dom out of range', '0 9 32 * *'],
    ['dom plus dow (cron OR semantics)', '0 9 15 * 1'],
    ['pinned month', '0 9 * 6 *'],
    ['month name', '0 9 * JAN *'],
    ['dow out of range', '0 9 * * 7'],
    ['dow wraparound range', '0 9 * * 5-1'],
    ['four fields', '0 9 * *'],
    ['six fields (seconds cron)', '0 0 9 * * *'],
    ['free text', 'not a cron'],
    ['empty string', ''],
    ['whitespace only', '   '],
    ['@daily descriptor', '@daily'],
    ['@every descriptor', '@every 1h'],
    ['L in dom', '0 9 L * *'],
    ['L in dow', '0 9 * * 5L'],
    ['hash nth-weekday', '0 9 * * 1#2'],
    ['W nearest-weekday', '0 9 15W * *'],
  ])('%s → raw preserved verbatim', (_label, expr) => {
    const p = parseCron(expr, TZ);
    expect(p.raw).toBe(expr);
    expect(p.timezone).toBe(TZ);
  });
});

describe('cronFields — the five-field serialization', () => {
  const base = getDefaultScheduleConfig(TZ);

  it.each<[string, ScheduleConfig, string]>([
    [
      'fixed daily time',
      { ...base, time: { kind: 'at', time: '09:00' }, days: { kind: 'every' } },
      '0 9 * * *',
    ],
    [
      'weekday range collapses',
      {
        ...base,
        time: { kind: 'at', time: '18:30' },
        days: { kind: 'weekly', daysOfWeek: [1, 2, 3, 4, 5] },
      },
      '30 18 * * 1-5',
    ],
    [
      'non-consecutive days stay a list',
      {
        ...base,
        time: { kind: 'at', time: '09:00' },
        days: { kind: 'weekly', daysOfWeek: [1, 3, 5] },
      },
      '0 9 * * 1,3,5',
    ],
    [
      'mixed runs and singletons',
      {
        ...base,
        time: { kind: 'at', time: '09:00' },
        days: { kind: 'weekly', daysOfWeek: [0, 1, 2, 4] },
      },
      '0 9 * * 0-2,4',
    ],
    [
      'flagship compound',
      {
        ...base,
        time: {
          kind: 'every',
          unit: 'hours',
          interval: 2,
          minute: 0,
          window: { from: '09:00', to: '21:00' },
        },
        days: { kind: 'weekly', daysOfWeek: [2, 3, 4] },
      },
      '0 9-21/2 * * 2-4',
    ],
    [
      'hourly with minute offset',
      {
        ...base,
        time: { kind: 'every', unit: 'hours', interval: 1, minute: 15, window: null },
        days: { kind: 'every' },
      },
      '15 * * * *',
    ],
    [
      'every N hours all day',
      {
        ...base,
        time: { kind: 'every', unit: 'hours', interval: 3, minute: 0, window: null },
        days: { kind: 'every' },
      },
      '0 */3 * * *',
    ],
    [
      'hour window without step',
      {
        ...base,
        time: {
          kind: 'every',
          unit: 'hours',
          interval: 1,
          minute: 30,
          window: { from: '09:30', to: '21:30' },
        },
        days: { kind: 'every' },
      },
      '30 9-21 * * *',
    ],
    [
      'minute interval with window',
      {
        ...base,
        time: {
          kind: 'every',
          unit: 'minutes',
          interval: 10,
          minute: 0,
          window: { from: '09:00', to: '18:59' },
        },
        days: { kind: 'every' },
      },
      '*/10 9-18 * * *',
    ],
    [
      'minute interval single hour',
      {
        ...base,
        time: {
          kind: 'every',
          unit: 'minutes',
          interval: 15,
          minute: 0,
          window: { from: '09:00', to: '09:59' },
        },
        days: { kind: 'every' },
      },
      '*/15 9 * * *',
    ],
    [
      'every minute',
      {
        ...base,
        time: { kind: 'every', unit: 'minutes', interval: 1, minute: 0, window: null },
        days: { kind: 'every' },
      },
      '* * * * *',
    ],
    [
      'monthly fixed time',
      {
        ...base,
        time: { kind: 'at', time: '10:30' },
        days: { kind: 'monthly', dayOfMonth: 15 },
      },
      '30 10 15 * *',
    ],
    [
      'hourly interval on a day of the month',
      {
        ...base,
        time: { kind: 'every', unit: 'hours', interval: 2, minute: 0, window: null },
        days: { kind: 'monthly', dayOfMonth: 15 },
      },
      '0 */2 15 * *',
    ],
    [
      'windowed hourly interval on a day of the month',
      {
        ...base,
        time: {
          kind: 'every',
          unit: 'hours',
          interval: 2,
          minute: 30,
          window: { from: '09:30', to: '21:30' },
        },
        days: { kind: 'monthly', dayOfMonth: 15 },
      },
      '30 9-21/2 15 * *',
    ],
    [
      'windowed minute interval on a day of the month',
      {
        ...base,
        time: {
          kind: 'every',
          unit: 'minutes',
          interval: 10,
          minute: 0,
          window: { from: '09:00', to: '18:59' },
        },
        days: { kind: 'monthly', dayOfMonth: 1 },
      },
      '*/10 9-18 1 * *',
    ],
    [
      'the whole week selected stays an explicit range',
      {
        ...base,
        time: { kind: 'at', time: '09:00' },
        days: { kind: 'weekly', daysOfWeek: [0, 1, 2, 3, 4, 5, 6] },
      },
      '0 9 * * 0-6',
    ],
  ])('%s', (_label, cfg, expected) => {
    expect(cronFields(cfg)).toBe(expected);
  });

  it('returns raw verbatim in advanced mode', () => {
    expect(cronFields({ ...base, raw: '0 9 1,15 * *' })).toBe('0 9 1,15 * *');
  });

  it('serialises weekly with no days to Monday as a safety fallback', () => {
    expect(
      cronFields({
        ...base,
        time: { kind: 'at', time: '09:00' },
        days: { kind: 'weekly', daysOfWeek: [] },
      }),
    ).toBe('0 9 * * 1');
  });
});

describe('bidirectional invariants', () => {
  const base = getDefaultScheduleConfig(TZ);
  const CANONICAL = [
    '0 9 * * *',
    '30 18 * * 1-5',
    '15 * * * *',
    '0 */2 * * *',
    '30 9-21 * * *',
    '0 9-21/2 * * 2-4',
    '*/10 9-18 * * 1-5',
    '*/15 9 * * *',
    '30 10 15 * *',
    '* * * * *',
    '0 9 * * 1,3,5',
    '0 9 * * 0-2,4',
    '0 */2 15 * *',
    '30 9-21/2 15 * *',
    '*/10 9-18 1 * *',
    '0 9 * * 0-6',
    '0 9-9 * * *',
    '0 9-9/3 * * *',
    '59 23 * * *',
  ];

  it.each(CANONICAL)('canonical form %s survives a verbatim round-trip', (expr) => {
    expect(cronFields(parseCron(expr, TZ))).toBe(expr);
  });

  const NORMALIZING: Array<[string, string]> = [
    ['05 09 * * *', '5 9 * * *'],
    ['0/5 * * * *', '*/5 * * * *'],
    ['*/10 */1 * * *', '*/10 * * * *'],
    ['*/10 0-23/1 * * *', '*/10 * * * *'],
    ['*/10 0-23 * * *', '*/10 * * * *'],
    ['0 0-23 * * *', '0 * * * *'],
    ['0 0-23/3 * * *', '0 */3 * * *'],
    ['15 0-23/2 * * 1-5', '15 */2 * * 1-5'],
    ['*/10 9-18/1 * * *', '*/10 9-18 * * *'],
    ['0 9-21/1 * * *', '0 9-21 * * *'],
    ['0 9 * * 5,1,5', '0 9 * * 1,5'],
    ['0 9 * * MON-FRI', '0 9 * * 1-5'],
    ['0 9 * * sun-sat', '0 9 * * 0-6'],
    ['0 9 * * 1-5,', '0 9 * * 1-5'],
    ['0 9 * * 1,,5', '0 9 * * 1,5'],
    ['0, 9 * * *', '0 9 * * *'],
    ['0 9, * * *', '0 9 * * *'],
    ['0 9 15, * *', '0 9 15 * *'],
    [',0 ,9 * * *', '0 9 * * *'],
    ['009 09 * * *', '9 9 * * *'],
    ['+30 14 * * *', '30 14 * * *'],
    ['00/05 9-18 * * *', '*/5 9-18 * * *'],
    ['0 09-021/02 * * *', '0 9-21/2 * * *'],
    ['0 9 015 * *', '0 9 15 * *'],
    ['0 9 * * 001', '0 9 * * 1'],
    ['0 9 * * MON-05', '0 9 * * 1-5'],
    ['0 9 * * */2', '0 9 * * 0,2,4,6'],
    ['0 9/2 * * *', '0 9-23/2 * * *'],
    ['0 ?/2 * * *', '0 */2 * * *'],
    ['0 9-9/24 * * *', '0 9 * * *'],
  ];

  it.each(NORMALIZING)('%s re-serialises to the semantically equal %s', (expr, normalized) => {
    expect(cronFields(parseCron(expr, TZ))).toBe(normalized);
  });

  it.each([...CANONICAL, ...NORMALIZING.map(([e]) => e)])(
    'parse ∘ toCron ∘ parse is idempotent for %s',
    (expr) => {
      const once = parseCron(expr, TZ);
      const twice = parseCron(toCron(once), TZ);
      expect(twice).toEqual(once);
    },
  );

  it('advanced expressions round-trip verbatim', () => {
    const p = parseCron('0 9 1,15 * *', TZ);
    expect(p.raw).toBe('0 9 1,15 * *');
    expect(cronFields(p)).toBe('0 9 1,15 * *');
    expect(parseCron(toCron(p), TZ)).toEqual(p);
  });

  const EDITOR_CONFIGS: Array<[string, ScheduleConfig]> = [
    [
      'a window dragged shut at interval 3',
      {
        ...base,
        time: {
          kind: 'every',
          unit: 'hours',
          interval: 3,
          minute: 0,
          window: { from: '09:00', to: '09:00' },
        },
      },
    ],
    [
      'the same window with a firing minute',
      {
        ...base,
        time: {
          kind: 'every',
          unit: 'hours',
          interval: 2,
          minute: 30,
          window: { from: '22:30', to: '22:30' },
        },
      },
    ],
    [
      'a minute-step window one hour wide',
      {
        ...base,
        time: {
          kind: 'every',
          unit: 'minutes',
          interval: 10,
          minute: 0,
          window: { from: '09:00', to: '09:59' },
        },
      },
    ],
    [
      'a window that stops one hour short of the day',
      {
        ...base,
        time: {
          kind: 'every',
          unit: 'hours',
          interval: 1,
          minute: 0,
          window: { from: '00:00', to: '22:00' },
        },
      },
    ],
    [
      'an all-day interval carrying a firing minute',
      { ...base, time: { kind: 'every', unit: 'hours', interval: 3, minute: 15, window: null } },
    ],
  ];

  it.each(EDITOR_CONFIGS)(
    'a config the editor can build survives cron and comes back: %s',
    (_label, config) => {
      expect(parseCron(toCron(config), TZ)).toEqual(config);
    },
  );
});

describe('toCron — the wire expression', () => {
  const base = getDefaultScheduleConfig(TZ);

  it('carries the timezone as a TZ= prefix on every serialization', () => {
    expect(toCron({ ...base, time: { kind: 'at', time: '09:00' } })).toBe(
      'TZ=Asia/Shanghai 0 9 * * *',
    );
    expect(toCron({ ...base, raw: '0 9 1,15 * *' })).toBe('TZ=Asia/Shanghai 0 9 1,15 * *');
  });

  it('round-trips through parseCron with no timezone fallback needed', () => {
    const config = { ...base, time: { kind: 'at', time: '09:00' } } as const;
    expect(parseCron(toCron(config), 'Test/Sentinel')).toEqual(config);
  });

  it('never stacks a second prefix on a raw that carries its own', () => {
    for (const raw of ['TZ=Local 0 9 * * *', 'CRON_TZ=Bogus/Zone 0 9 * * *', 'TZ=UTC']) {
      expect(toCron({ ...base, raw })).toBe(raw);
    }
  });

  it('degrades to the bare fields when the zone could not ride in a prefix', () => {
    expect(toCron({ ...base, timezone: '' })).toBe('0 9 * * *');
    expect(toCron({ ...base, timezone: 'Bad Zone' })).toBe('0 9 * * *');
  });
});

describe('parseCron — timezone prefix extraction', () => {
  it('extracts TZ= into the timezone and structures the rest', () => {
    const p = parseCron('TZ=Asia/Tokyo 0 9 * * *', TZ);
    expect(p.raw).toBeNull();
    expect(p.timezone).toBe('Asia/Tokyo');
    expect(p.time).toEqual({ kind: 'at', time: '09:00' });
    expect(toCron(p)).toBe('TZ=Asia/Tokyo 0 9 * * *');
  });

  it('extracts CRON_TZ= the same way, and re-serialises it as TZ=', () => {
    const p = parseCron('CRON_TZ=America/New_York 30 */3 * * 1-5', TZ);
    expect(p.raw).toBeNull();
    expect(p.timezone).toBe('America/New_York');
    expect(toCron(p)).toBe('TZ=America/New_York 30 */3 * * 1-5');
  });

  it('reads an empty zone as the UTC it loads as', () => {
    expect(parseCron('TZ= 0 9 * * *', TZ).timezone).toBe('UTC');
  });

  it("canonicalizes the zone's spelling to the one the picker's list uses", () => {
    expect(parseCron('TZ=asia/shanghai 0 9 * * *', 'UTC').timezone).toBe('Asia/Shanghai');
    expect(parseCron('CRON_TZ=UTC 0 9 * * *', 'Asia/Tokyo').timezone).toBe('UTC');
  });

  it('extracts over an advanced-only body too, stripping the prefix from raw', () => {
    const p = parseCron('TZ=Asia/Tokyo ?/2 ?/2 * * ?/2', TZ);
    expect(p.raw).toBe('?/2 ?/2 * * ?/2');
    expect(p.timezone).toBe('Asia/Tokyo');
  });

  it.each([
    ['a zone only the server knows', 'TZ=Local 0 9 * * *'],
    ['a zone nobody knows', 'TZ=Bogus/Zone 0 9 * * *'],
    ['a tab-ridden zone', 'TZ=UTC\t 0 9 * * *'],
    ['prefix without a schedule', 'TZ=UTC'],
    ['CRON_TZ prefix without a schedule', 'CRON_TZ=Asia/Tokyo'],
    ['bare TZ=', 'TZ='],
    ['prefix with only trailing space', 'TZ=UTC '],
    ['a second prefix behind the first', 'TZ=UTC TZ=UTC 0 9 * * *'],
    ['a CRON_TZ prefix behind a TZ one', 'TZ=UTC CRON_TZ=Asia/Tokyo 0 9 * * *'],
    ['lowercase tz=', 'tz=UTC 0 9 * * *'],
    ['leading space before the prefix', ' TZ=UTC 0 9 * * *'],
  ])('%s stays verbatim in advanced mode', (_label, expr) => {
    const p = parseCron(expr, TZ);
    expect(p.raw).toBe(expr);
    expect(p.timezone).toBe(TZ);
  });
});
