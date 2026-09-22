import { describe, it, expect, vi } from 'vitest';
import { cronFields, parseCron, toCron } from './cron-mapping';

vi.setConfig({ testTimeout: 30_000 });

interface Bounds {
  min: number;
  max: number;
  names?: Record<string, number>;
}

const MONTH_NAMES = {
  jan: 1,
  feb: 2,
  mar: 3,
  apr: 4,
  may: 5,
  jun: 6,
  jul: 7,
  aug: 8,
  sep: 9,
  oct: 10,
  nov: 11,
  dec: 12,
};
const DOW_NAMES = { sun: 0, mon: 1, tue: 2, wed: 3, thu: 4, fri: 5, sat: 6 };

const MINUTE: Bounds = { min: 0, max: 59 };
const HOUR: Bounds = { min: 0, max: 23 };
const DOM: Bounds = { min: 1, max: 31 };
const MONTH: Bounds = { min: 1, max: 12, names: MONTH_NAMES };
const DOW: Bounds = { min: 0, max: 6, names: DOW_NAMES };

interface Field {
  values: Set<number>;
  star: boolean;
}

class CronSyntaxError extends Error {}

function parseIntOrName(token: string, b: Bounds): number {
  const named = b.names?.[token.toLowerCase()];
  if (named !== undefined) return named;
  if (!/^\+?\d+$/.test(token)) throw new CronSyntaxError(`bad token ${token}`);
  return parseInt(token, 10);
}

function getRange(expr: string, b: Bounds): Field {
  const parts = expr.split('/');
  if (parts.length > 2) throw new CronSyntaxError(`too many slashes in ${expr}`);
  const rangeStr = parts[0]!;
  if (rangeStr === '') throw new CronSyntaxError(`empty range in ${expr}`);

  let step = 1;
  if (parts.length === 2) {
    const stepStr = parts[1]!;
    if (!/^\+?\d+$/.test(stepStr)) throw new CronSyntaxError(`bad step ${stepStr}`);
    step = parseInt(stepStr, 10);
    if (step === 0) throw new CronSyntaxError('step of zero');
  }

  const ends = rangeStr.split('-');
  const star = ends[0] === '*' || ends[0] === '?';
  let low: number;
  let high: number;
  if (star) {
    low = b.min;
    high = b.max;
  } else {
    if (ends.length > 2) throw new CronSyntaxError(`too many dashes in ${expr}`);
    low = parseIntOrName(ends[0]!, b);
    if (ends.length === 2) {
      high = parseIntOrName(ends[1]!, b);
    } else {
      high = parts.length === 2 ? b.max : low;
    }
  }
  if (low < b.min || high > b.max) throw new CronSyntaxError(`${expr} out of bounds`);
  if (low > high) throw new CronSyntaxError(`${expr} runs backwards`);

  const values = new Set<number>();
  for (let v = low; v <= high; v += step) values.add(v);
  return { values, star: star && step === 1 };
}

function parseField(expr: string, b: Bounds): Field {
  const values = new Set<number>();
  let star = false;
  for (const part of expr.split(',').filter((p) => p.length > 0)) {
    const f = getRange(part, b);
    for (const v of f.values) values.add(v);
    if (f.star) star = true;
  }
  return { values, star };
}

interface Spec {
  minute: Field;
  hour: Field;
  dom: Field;
  month: Field;
  dow: Field;
  tz: string | null;
  tzValid: boolean;
  body: string;
}

const TZ_VERDICTS: Record<string, boolean> = {
  UTC: true,
  'Asia/Tokyo': true,
  'Asia/Shanghai': true,
  Local: true,
  'Bogus/Zone': false,
  'A=B': false,
  'UTC\t': false,
};

function referencePrefix(
  expr: string,
): { tz: string; tzValid: boolean; rest: string } | 'reject' | null {
  if (!expr.startsWith('TZ=') && !expr.startsWith('CRON_TZ=')) return null;
  const space = expr.indexOf(' ');
  if (space === -1) return 'reject';
  const name = expr.slice(expr.indexOf('=') + 1, space);
  const tzValid = TZ_VERDICTS[name === '' ? 'UTC' : name];
  if (tzValid === undefined)
    throw new Error(`corpus uses unpinned timezone ${JSON.stringify(name)}`);
  return { tz: name === '' ? 'UTC' : name, tzValid, rest: expr.slice(space).trim() };
}

function reference(expr: string): Spec | null {
  const prefix = referencePrefix(expr);
  if (prefix === 'reject') return null;
  const body = prefix === null ? expr : prefix.rest;
  const parts = body
    .trim()
    .split(/\s+/)
    .filter((p) => p.length > 0);
  if (parts.length !== 5) return null;
  try {
    return {
      minute: parseField(parts[0]!, MINUTE),
      hour: parseField(parts[1]!, HOUR),
      dom: parseField(parts[2]!, DOM),
      month: parseField(parts[3]!, MONTH),
      dow: parseField(parts[4]!, DOW),
      tz: prefix === null ? null : prefix.tz,
      tzValid: prefix === null ? true : prefix.tzValid,
      body: body.trim(),
    };
  } catch (err) {
    if (err instanceof CronSyntaxError) return null;
    throw err;
  }
}

function serverAccepts(spec: Spec | null): spec is Spec {
  return spec !== null && spec.tzValid;
}

function extractedForEcho(expr: string): { tz: string; rest: string } | null {
  const p = referencePrefix(expr);
  if (p === null || p === 'reject') return null;
  if (p.rest.length === 0) return null;
  if (p.rest.startsWith('TZ=') || p.rest.startsWith('CRON_TZ=')) return null;
  const canonical = canonicalPickerZone(p.tz);
  if (canonical === null) return null;
  return { tz: canonical, rest: p.rest };
}

function canonicalPickerZone(tz: string): string | null {
  try {
    return new Intl.DateTimeFormat(undefined, { timeZone: tz }).resolvedOptions().timeZone;
  } catch {
    return null;
  }
}

function dayMatches(spec: Spec, dom: number, dow: number): boolean {
  const domHit = spec.dom.values.has(dom);
  const dowHit = spec.dow.values.has(dow);
  return spec.dom.star || spec.dow.star ? domHit && dowHit : domHit || dowHit;
}

function sameSet(a: Set<number>, b: Set<number>): boolean {
  return a.size === b.size && [...a].every((v) => b.has(v));
}

function sameSchedule(a: Spec, b: Spec): boolean {
  if (!sameSet(a.minute.values, b.minute.values)) return false;
  if (!sameSet(a.hour.values, b.hour.values)) return false;
  if (!sameSet(a.month.values, b.month.values)) return false;
  for (let dom = 1; dom <= 31; dom++) {
    for (let dow = 0; dow <= 6; dow++) {
      if (dayMatches(a, dom, dow) !== dayMatches(b, dom, dow)) return false;
    }
  }
  return true;
}

const MINUTE_TOKENS = [
  '*',
  '?',
  '0',
  '5',
  '30',
  '59',
  '60',
  '-5',
  '*/1',
  '*/5',
  '*/59',
  '*/60',
  '*/0',
  '?/5',
  '0/5',
  '15/5',
  '59/5',
  '0-30',
  '0-30/10',
  '30-0',
  '0-59',
  '0,30',
  '0,15,30,45',
  '0-10,30',
  '0-30/10,45',
  '0,30,',
  ',0,30',
  '1,,5',
  ',',
  '0,',
  ',0',
  '30,',
  '0,*',
  '*,*/2',
  '?,15',
  '0,*-30',
  '*,60',
  '*,abc',
  '09',
  '009',
  '00',
  '+30',
  '030-045',
  '00/05',
  '*/05',
  '*/+5',
  '99999999999999999999',
  '+',
  '*-30',
  '?-30',
  '*-30/10',
  '?-30/10',
  '*-99',
  '*-abc',
  '*-10-20',
  '9-?',
  '5-5',
  '',
  '*/',
  '0//5',
  '0--30',
  'abc',
];

const HOUR_TOKENS = [
  '*',
  '?',
  '0',
  '9',
  '23',
  '24',
  '-1',
  '*/1',
  '*/2',
  '*/23',
  '*/24',
  '*/0',
  '0/2',
  '9/2',
  '23/2',
  '9/1',
  '?/2',
  '*-19',
  '?-19',
  '*-19/2',
  '?-19/2',
  '*-19/23',
  '9-19/23',
  '9-21',
  '9-21/2',
  '9-21/1',
  '21-9',
  '0-23',
  '0-23/1',
  '9-9',
  '9-9/3',
  '9-9/24',
  '9-11/5',
  '9-21/23',
  '9-21/30',
  '9,12,15',
  '9-12,18',
  '0-23/6',
  '9,12,',
  '9,,12',
  '9,',
  ',9',
  '0,',
  '9,*',
  '*,*/2',
  '09',
  '021',
  '+9',
  '09-021/02',
  '',
  '*/',
  '9//2',
  'abc',
];

const DOM_TOKENS = [
  '*',
  '?',
  '1',
  '15',
  '31',
  '0',
  '32',
  '*/2',
  '?/2',
  '1/2',
  '1-15',
  '1-15/2',
  '15-1',
  '*-15',
  '?-15',
  '*-15/2',
  '?/40',
  '1,15',
  '1-7,15',
  '1,15,',
  '1,,15',
  '15,',
  ',15',
  '1,*',
  '*,32',
  'L',
  '15W',
  '015',
  '01',
  '+15',
  '',
  'abc',
];

const MONTH_TOKENS = [
  '*',
  '?',
  '1',
  '6',
  '12',
  '0',
  '13',
  'JAN',
  'jan',
  'DEC',
  'FOO',
  '*/2',
  '?/2',
  '1-6',
  '1-6/2',
  '1,6',
  '6-1',
  '1,6,',
  '1,,6',
  '6,',
  ',6',
  '1,*',
  'JAN,*',
  'FOO,*',
  '*-6',
  '?-6',
  '*-6/2',
  'JAN-JUN/2',
  '06',
  '012',
  '+6',
  '',
  'abc',
];

const DOW_TOKENS = [
  '*',
  '?',
  '0',
  '1',
  '6',
  '7',
  '-1',
  'SUN',
  'sun',
  'SAT',
  'MON',
  'FOO',
  '*/2',
  '*/7',
  '?/2',
  '1/2',
  'MON/2',
  '0/3',
  '*-5',
  '?-5',
  '*-5/2',
  '?-5/2',
  'SUN-SAT/2',
  '*/100',
  '1-5/60',
  'MON-FRI/60',
  '1-5',
  'MON-FRI',
  'mon-fri',
  'sun-sat',
  '0-6',
  '5-1',
  '1-5/2',
  '1-1',
  '1,3,5',
  'MON,WED',
  '5,1,5',
  '1,3-5',
  '0-2,4',
  '1-5,',
  ',1,5',
  '1,,5',
  ',',
  '1,',
  ',1',
  '5,',
  '1,*',
  'MON,*',
  '*/2,*/3',
  '*,7',
  '001',
  '01',
  '+1',
  '+0',
  'MON-05',
  '0-06/02',
  '1#2',
  '5L',
  '',
  'abc',
];

const DAY_BRANCHES = ['* * *', '* * 1-5', '* * 0', '* * 6', '1 * *', '15 * *', '31 * *', '15 * 1'];

const TIME_BRANCHES = [
  '0 9',
  '30 14',
  '0 *',
  '15 *',
  '0 */2',
  '30 */3',
  '0 9-21',
  '30 9-21',
  '0 9-21/2',
  '* *',
  '*/10 *',
  '*/10 9-18',
  '*/15 9',
];

function corpus(): string[] {
  const out = new Set<string>();

  for (const m of MINUTE_TOKENS) {
    for (const h of HOUR_TOKENS) {
      for (const days of DAY_BRANCHES) out.add(`${m} ${h} ${days}`);
    }
  }

  for (const dom of DOM_TOKENS) {
    for (const dow of DOW_TOKENS) {
      for (const time of TIME_BRANCHES) out.add(`${time} ${dom} * ${dow}`);
    }
  }

  for (const mo of MONTH_TOKENS) {
    for (const time of TIME_BRANCHES) out.add(`${time} * ${mo} *`);
    out.add(`0 9 15 ${mo} *`);
    out.add(`0 9 * ${mo} 1-5`);
  }

  for (const e of [
    '',
    '   ',
    '0 9 * *',
    '0 9 * * * *',
    '0 0 9 * * *',
    '@daily',
    '@hourly',
    '@every 1h',
    '@reboot',
    'not a cron',
    '0 9 * * 1 # comment',
    '  0  9  *  *  *  ',
  ]) {
    out.add(e);
  }

  const TZ_PREFIXES = [
    'TZ=UTC',
    'CRON_TZ=Asia/Tokyo',
    'TZ=Local',
    'TZ=',
    'TZ=Bogus/Zone',
    'TZ=A=B',
    'tz=UTC',
    'TZ=UTC\t',
  ];
  const TZ_BODIES = [
    '0 9 * * *',
    '30 */3 * * 1-5',
    '0 9 1,15 * *',
    '?/2 ?/2 * * ?/2',
    'not a cron',
    '0 9 * *',
    '',
    '@daily',
  ];
  for (const p of TZ_PREFIXES) {
    for (const b of TZ_BODIES) out.add(`${p} ${b}`);
  }
  for (const e of [
    'TZ=UTC',
    'CRON_TZ=Asia/Tokyo',
    'TZ=',
    'TZ=UTC ',
    'TZ=UTC   ',
    'TZ=UTC TZ=UTC 0 9 * * *',
    'TZ=UTC CRON_TZ=Asia/Tokyo 0 9 * * *',
    ' TZ=UTC 0 9 * * *',
  ]) {
    out.add(e);
  }

  return [...out];
}

const CORPUS = corpus();

const EXCLUSIONS: Array<{ why: string; holds: (spec: Spec, fields: string[]) => boolean }> = [
  {
    why: 'a field that selects no value at all, so the schedule never fires',
    holds: (s) => [s.minute, s.hour, s.dom, s.month, s.dow].some((f) => f.values.size === 0),
  },
  {
    why: 'minute is a list, a range, or a step anchored off zero — the model has no minute-offset slot',
    holds: (_s, f) => {
      const m = f[0]!.replace(/^\?(?=\/|$)/, '*');
      if (m === '*') return false;
      if (/^\d{1,2}$/.test(m)) return false;
      return !/^(\*|0)\/\d{1,2}$/.test(m);
    },
  },
  {
    why: 'hour is a list',
    holds: (_s, f) => {
      const h = f[1]!.replace(/^\?(?=\/|$)/, '*');
      return !(
        h === '*' ||
        /^\d{1,2}$/.test(h) ||
        /^\d{1,2}-\d{1,2}(\/\d{1,2})?$/.test(h) ||
        /^\*\/\d{1,2}$/.test(h) ||
        /^\d{1,2}\/\d{1,2}$/.test(h)
      );
    },
  },
  {
    why: "a minute step and an hour step at once — the model's interval has one step dimension",
    holds: (_s, f) => {
      const m = f[0]!.replace(/^\?(?=\/|$)/, '*');
      const minuteStepped = m === '*' || /^(\*|0)\/\d{1,2}$/.test(m);
      const hourStep = /\/(\d{1,2})$/.exec(f[1]!);
      return minuteStepped && hourStep !== null && parseInt(hourStep[1]!, 10) !== 1;
    },
  },
  {
    why: 'day-of-month is anything but a single day',
    holds: (_s, f) => !(f[2] === '*' || /^\d{1,2}$/.test(f[2]!)),
  },
  {
    why: 'a pinned day-of-month beside a restricted day-of-week — cron ORs them, the model can only intersect',
    holds: (_s, f) => f[2] !== '*' && f[4] !== '*',
  },
  { why: 'month is pinned', holds: (_s, f) => f[3] !== '*' },
  {
    why: 'an embedded timezone the picker cannot offer',
    holds: (s) => s.tz !== null && canonicalPickerZone(s.tz) === null,
  },
];

function meaningfulFields(expr: string, spec: Spec): string[] {
  const fields = [spec.minute, spec.hour, spec.dom, spec.month, spec.dow];
  return expr
    .trim()
    .split(/\s+/)
    .map((field, i) => {
      const parsed = fields[i]!;
      if (parsed.star) return '*';
      if (parsed.values.size === 1) return String([...parsed.values][0]);
      return field
        .split(',')
        .filter((part) => part.length > 0)
        .map((part) =>
          part.replace(/\+?\d+/g, (num) => {
            const n = Number(num);
            return Number.isSafeInteger(n) ? String(n) : num;
          }),
        )
        .join(',');
    });
}

describe('cron grammar — the editor against a reference robfig parser', () => {
  it('covers every token form of every field', () => {
    expect(CORPUS.length).toBeGreaterThan(10_000);
    expect(CORPUS.filter((e) => reference(e) !== null).length).toBeGreaterThan(3000);
    expect(CORPUS.filter((e) => reference(e) === null).length).toBeGreaterThan(3000);
    expect(CORPUS).toContain('0 */2 15 * *');
    expect(CORPUS).toContain('*/10 9-18 15 * *');
    expect(CORPUS).toContain('0 9-21/2 * * 1-5');
    expect(CORPUS).toContain('009 9 * * *');
    expect(CORPUS).toContain('0 9 * * +1');
    expect(CORPUS).toContain('*-30 9 * * *');
    expect(CORPUS).toContain('0 *-19 * * *');
    expect(CORPUS).toContain('0 9 *-15 * *');
    expect(CORPUS).toContain('0 9 * *-6 *');
    expect(CORPUS).toContain('0 9 * * ?-5');
    expect(CORPUS).toContain('0,* 9 * * *');
    expect(CORPUS).toContain('0 9,* * * *');
    expect(CORPUS).toContain('0 9 1,* * *');
    expect(CORPUS).toContain('0 9 * 1,* *');
    expect(CORPUS).toContain('0 9 * * 1,*');
    expect(CORPUS).toContain('*,abc 9 * * *');
    expect(CORPUS).toContain('TZ=UTC 0 9 * * *');
    expect(CORPUS).toContain('CRON_TZ=Asia/Tokyo 30 */3 * * 1-5');
    expect(CORPUS).toContain('TZ=Bogus/Zone 0 9 * * *');
    expect(CORPUS).toContain('TZ=UTC');
    expect(CORPUS).toContain('tz=UTC 0 9 * * *');
  });

  it('reads the timezone prefix the way the server does', () => {
    expect(reference('TZ=UTC 0 9 * * *')).toMatchObject({ tz: 'UTC', tzValid: true });
    expect(reference('CRON_TZ=Asia/Tokyo 0 9 * * *')).toMatchObject({ tz: 'Asia/Tokyo' });
    expect(reference('TZ= 0 9 * * *')).toMatchObject({ tz: 'UTC', tzValid: true });
    expect(reference('TZ=Bogus/Zone 0 9 * * *')).toMatchObject({ tzValid: false });
    expect(reference('TZ=UTC')).toBeNull();
    expect(reference('CRON_TZ=Asia/Tokyo')).toBeNull();
    expect(reference('TZ=')).toBeNull();
    expect(reference('tz=UTC 0 9 * * *')).toBeNull();
    expect(reference(' TZ=UTC 0 9 * * *')).toBeNull();
    expect(reference('TZ=UTC TZ=UTC 0 9 * * *')).toBeNull();
  });

  it("reads a wildcard's low end the way the server does", () => {
    expect(reference('0 *-19 * * *')?.hour.values.size).toBe(24);
    expect(reference('0 ?-19 * * *')?.hour.star).toBe(true);
    expect(reference('0 *-19-25 * * *')?.hour.values.size).toBe(24);
    expect([...(reference('0 *-19/2 * * *')?.hour.values ?? [])]).toEqual([
      0, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20, 22,
    ]);
    expect(reference('0 9-? * * *')).toBeNull();
  });

  it('never throws, whatever it is handed', () => {
    for (const expr of CORPUS) {
      expect(() => parseCron(expr, 'UTC'), expr).not.toThrow();
    }
  });

  it('never structures an expression whose fields the server cannot parse', () => {
    const wronglyAccepted = CORPUS.filter(
      (e) => reference(e) === null && parseCron(e, 'UTC').raw === null,
    );
    expect(wronglyAccepted).toEqual([]);
  });

  it('keeps an embedded zone in the config, so the server still judges the pair', () => {
    const wrongTz = CORPUS.filter((e) => {
      const p = extractedForEcho(e);
      return parseCron(e, 'Test/Sentinel').timezone !== (p?.tz ?? 'Test/Sentinel');
    });
    expect(wrongTz).toEqual([]);
  });

  it("never structures a config the model's own domain cannot hold", () => {
    const validTime = (t: string) => /^([01]\d|2[0-3]):[0-5]\d$/.test(t);
    const outOfDomain: Array<{ expr: string; why: string }> = [];
    for (const expr of CORPUS) {
      const c = parseCron(expr, 'UTC');
      if (c.raw !== null) continue;
      const bad = (why: string) => outOfDomain.push({ expr, why });
      const { time, days } = c;
      if (time.kind === 'at') {
        if (!validTime(time.time)) bad('at-time malformed');
      } else {
        const maxInterval = time.unit === 'hours' ? 23 : 59;
        if (!Number.isInteger(time.interval) || time.interval < 1 || time.interval > maxInterval) {
          bad(`interval ${time.interval} outside 1-${maxInterval}`);
        }
        if (!Number.isInteger(time.minute) || time.minute < 0 || time.minute > 59) {
          bad(`minute ${time.minute} outside 0-59`);
        }
        if (time.window !== null && (!validTime(time.window.from) || !validTime(time.window.to))) {
          bad('window malformed');
        }
      }
      if (days.kind === 'weekly') {
        const chips = days.daysOfWeek;
        if (chips.length === 0) bad('weekly with no days');
        if (chips.some((d) => !Number.isInteger(d) || d < 0 || d > 6)) bad('day outside 0-6');
        const canonical = [...new Set(chips)].toSorted((a, b) => a - b);
        if (JSON.stringify(canonical) !== JSON.stringify(chips)) bad('days not deduped ascending');
      } else if (days.kind === 'monthly') {
        if (!Number.isInteger(days.dayOfMonth) || days.dayOfMonth < 1 || days.dayOfMonth > 31) {
          bad(`dayOfMonth ${days.dayOfMonth} outside 1-31`);
        }
      }
    }
    expect(outOfDomain).toEqual([]);
  });

  it("preserves the schedule's meaning through every structurable round-trip", () => {
    const drifted: Array<{ expr: string; became: string; tz: string }> = [];
    for (const expr of CORPUS) {
      const config = parseCron(expr, 'UTC');
      if (config.raw !== null) continue;
      const before = reference(expr);
      const after = reference(toCron(config));
      if (
        before === null ||
        after === null ||
        !sameSchedule(before, after) ||
        config.timezone !== (before.tz ?? 'UTC')
      ) {
        drifted.push({ expr, became: toCron(config), tz: config.timezone });
      }
    }
    expect(drifted).toEqual([]);
  });

  it('hands back an unrepresentable expression as it was written, minus only an extracted prefix', () => {
    const mangled = CORPUS.filter((e) => {
      const c = parseCron(e, 'Test/Sentinel');
      if (c.raw === null) return false;
      const p = extractedForEcho(e);
      const expectedRaw = p === null ? e : p.rest;
      return c.raw !== expectedRaw || cronFields(c) !== expectedRaw;
    });
    expect(mangled).toEqual([]);
  });

  it('is idempotent: a second pass changes nothing', () => {
    const unstable = CORPUS.filter((expr) => {
      const once = parseCron(expr, 'UTC');
      const twice = parseCron(toCron(once), once.timezone);
      return JSON.stringify(twice) !== JSON.stringify(once);
    });
    expect(unstable).toEqual([]);
  });

  it('sends a valid expression to advanced-only for a documented reason, never by accident', () => {
    const unexplained: string[] = [];
    for (const expr of CORPUS) {
      const spec = reference(expr);
      if (!serverAccepts(spec)) continue;
      if (parseCron(expr, 'UTC').raw === null) continue;
      if (!EXCLUSIONS.some((x) => x.holds(spec, meaningfulFields(spec.body, spec)))) {
        unexplained.push(expr);
      }
    }
    expect(unexplained).toEqual([]);
  });
});
