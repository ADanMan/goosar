import type { DayPattern, ScheduleConfig, TimePattern } from './model';
import { consecutiveRuns, getDefaultScheduleConfig, pad2, timeParts } from './model';

const DOW_NAMES: Record<string, number> = {
  SUN: 0,
  MON: 1,
  TUE: 2,
  WED: 3,
  THU: 4,
  FRI: 5,
  SAT: 6,
};

const MONTH_NAMES: Record<string, number> = {
  JAN: 1,
  FEB: 2,
  MAR: 3,
  APR: 4,
  MAY: 5,
  JUN: 6,
  JUL: 7,
  AUG: 8,
  SEP: 9,
  OCT: 10,
  NOV: 11,
  DEC: 12,
};

interface FieldBounds {
  min: number;
  max: number;
  names?: Record<string, number>;
}

const MINUTE: FieldBounds = { min: 0, max: 59 };
const HOUR: FieldBounds = { min: 0, max: 23 };
const DOM: FieldBounds = { min: 1, max: 31 };
const MONTH: FieldBounds = { min: 1, max: 12, names: MONTH_NAMES };
const DOW: FieldBounds = { min: 0, max: 6, names: DOW_NAMES };

interface RangePart {
  lo: number;
  hi: number;
  step: number;
  wildcard: boolean;
  stepped: boolean;
  explicitRange: boolean;
}

function fieldValue(s: string, bounds: FieldBounds): number | null {
  const named = bounds.names?.[s.toUpperCase()];
  if (named !== undefined) return named;
  if (!/^\d+$/.test(s)) return null;
  const n = Number(s);
  if (!Number.isSafeInteger(n) || n < bounds.min || n > bounds.max) return null;
  return n;
}

function parseRangePart(part: string, bounds: FieldBounds): RangePart | null {
  const [rangeStr, stepStr, ...rest] = part.split('/');
  if (rest.length > 0 || !rangeStr) return null;

  let step = 1;
  if (stepStr !== undefined) {
    if (!/^\d+$/.test(stepStr)) return null;
    step = Number(stepStr);
    if (!Number.isSafeInteger(step) || step < 1) return null;
  }
  const stepped = stepStr !== undefined;

  if (rangeStr === '*') {
    return {
      lo: bounds.min,
      hi: bounds.max,
      step,
      wildcard: true,
      stepped,
      explicitRange: false,
    };
  }

  const [loStr, hiStr, ...restRange] = rangeStr.split('-');
  if (restRange.length > 0 || !loStr) return null;
  const lo = fieldValue(loStr, bounds);
  if (lo === null) return null;

  let hi: number;
  if (hiStr === undefined) {
    hi = stepped ? bounds.max : lo;
  } else {
    const hiVal = fieldValue(hiStr, bounds);
    if (hiVal === null) return null;
    hi = hiVal;
  }
  if (hi < lo) return null;

  return { lo, hi, step, wildcard: false, stepped, explicitRange: hiStr !== undefined };
}

function plainValue(field: string, bounds: FieldBounds): number | null {
  const r = parseRangePart(field, bounds);
  if (r === null || r.wildcard || r.stepped || r.explicitRange) return null;
  return r.lo;
}

function expandDow(field: string): number[] | null {
  const days = new Set<number>();
  for (const part of field.split(',')) {
    const r = parseRangePart(part, DOW);
    if (r === null) return null;
    for (let d = r.lo; d <= r.hi; d += r.step) days.add(d);
  }
  if (days.size === 0) return null;
  return Array.from(days).toSorted((a, b) => a - b);
}

function dowFieldFromDays(days: number[]): string {
  return consecutiveRuns(days)
    .map(([lo, hi]) => (hi > lo ? `${lo}-${hi}` : `${lo}`))
    .join(',');
}

function parseHourPattern(
  field: string,
): { interval: number; range: { lo: number; hi: number } | null } | null {
  const r = parseRangePart(field, HOUR);
  if (r === null || r.step > HOUR.max) return null;
  const spansDay = r.wildcard || (r.lo === HOUR.min && r.hi === HOUR.max);
  return { interval: r.step, range: spansDay ? null : { lo: r.lo, hi: r.hi } };
}

function parseMinuteInterval(field: string): number | null {
  const r = parseRangePart(field, MINUTE);
  if (r === null || r.explicitRange || r.step > MINUTE.max) return null;
  if (r.lo !== MINUTE.min) return null;
  if (!r.stepped && !r.wildcard) return null;
  return r.step;
}

function parseTimeFields(minuteField: string, hourField: string): TimePattern | null {
  const plainMinute = plainValue(minuteField, MINUTE);

  if (plainMinute !== null) {
    const plainHour = plainValue(hourField, HOUR);
    if (plainHour !== null) {
      return { kind: 'at', time: `${pad2(plainHour)}:${pad2(plainMinute)}` };
    }
    const hour = parseHourPattern(hourField);
    if (hour === null) return null;
    return {
      kind: 'every',
      unit: 'hours',
      interval: hour.interval,
      minute: plainMinute,
      window:
        hour.range === null
          ? null
          : {
              from: `${pad2(hour.range.lo)}:${pad2(plainMinute)}`,
              to: `${pad2(hour.range.hi)}:${pad2(plainMinute)}`,
            },
    };
  }

  const interval = parseMinuteInterval(minuteField);
  if (interval === null) return null;
  let window: { from: string; to: string } | null;
  const hour = parseHourPattern(hourField);
  if (hour === null) return null;
  if (hour.range === null) {
    if (hour.interval !== 1) return null;
    window = null;
  } else {
    const { lo, hi } = hour.range;
    const selectsOneHour = hour.interval > hi - lo;
    if (!selectsOneHour && hour.interval !== 1) return null;
    window = { from: `${pad2(lo)}:00`, to: `${pad2(selectsOneHour ? lo : hi)}:59` };
  }
  return { kind: 'every', unit: 'minutes', interval, minute: 0, window };
}

function normalizeField(field: string): string {
  return field
    .split(',')
    .filter((part) => part.length > 0)
    .map(normalizeRange)
    .map((part) =>
      part.replace(/\+?\d+/g, (num) => {
        const n = Number(num);
        return Number.isSafeInteger(n) ? String(n) : num;
      }),
    )
    .join(',');
}

function normalizeRange(part: string): string {
  const slash = part.indexOf('/');
  const range = slash === -1 ? part : part.slice(0, slash);
  const step = slash === -1 ? '' : part.slice(slash);
  const low = range.split('-')[0];
  return low === '*' || low === '?' ? `*${step}` : part;
}

function collapseDegenerateRange(field: string, bounds: FieldBounds, rangeSlot: boolean): string {
  return field
    .split(',')
    .map((part) => {
      const r = parseRangePart(part, bounds);
      if (r === null) return part;
      if (rangeSlot && !r.stepped) return part;
      if (rangeSlot && r.explicitRange && r.step <= bounds.max) return part;
      return r.step > r.hi - r.lo ? String(r.lo) : part;
    })
    .join(',');
}

function collapseWildcardList(field: string, bounds: FieldBounds): string {
  const parts = field.split(',');
  if (parts.length < 2) return field;
  const ranges = parts.map((part) => parseRangePart(part, bounds));
  if (ranges.some((r) => r === null)) return field;
  return ranges.some((r) => r!.wildcard && r!.step === 1) ? '*' : field;
}

export function hasTimezonePrefix(s: string): boolean {
  return s.startsWith('TZ=') || s.startsWith('CRON_TZ=');
}

export function extractTimezonePrefix(expr: string): { timezone: string; rest: string } | null {
  if (!hasTimezonePrefix(expr)) return null;
  const space = expr.indexOf(' ');
  if (space === -1) return null;
  const name = expr.slice(expr.indexOf('=') + 1, space);
  const rest = expr.slice(space).trim();
  if (rest.length === 0) return null;
  if (hasTimezonePrefix(rest)) return null;
  const timezone = canonicalPickerZone(name === '' ? 'UTC' : name);
  if (timezone === null) return null;
  return { timezone, rest };
}

function canonicalPickerZone(tz: string): string | null {
  try {
    return new Intl.DateTimeFormat(undefined, { timeZone: tz }).resolvedOptions().timeZone;
  } catch {
    return null;
  }
}

export function parseCron(expr: string, timezone: string): ScheduleConfig {
  const prefix = extractTimezonePrefix(expr);
  const tz = prefix === null ? timezone : prefix.timezone;
  const body = prefix === null ? expr : prefix.rest;
  const advanced: ScheduleConfig = {
    ...getDefaultScheduleConfig(tz),
    raw: body,
  };
  const parts = body.trim().split(/\s+/);
  if (parts.length !== 5) return advanced;
  const [rawMinute, rawHour, rawDom, rawMonth, rawDow] = parts.map(normalizeField) as [
    string,
    string,
    string,
    string,
    string,
  ];
  const minuteField = collapseDegenerateRange(
    collapseWildcardList(rawMinute, MINUTE),
    MINUTE,
    false,
  );
  const hourField = collapseDegenerateRange(collapseWildcardList(rawHour, HOUR), HOUR, true);
  const domField = collapseDegenerateRange(collapseWildcardList(rawDom, DOM), DOM, false);
  const monthField = collapseWildcardList(rawMonth, MONTH);
  const dowField = collapseWildcardList(rawDow, DOW);

  if (monthField !== '*') return advanced;

  let days: DayPattern;
  if (domField === '*') {
    if (dowField === '*') {
      days = { kind: 'every' };
    } else {
      const daysOfWeek = expandDow(dowField);
      if (daysOfWeek === null) return advanced;
      days = { kind: 'weekly', daysOfWeek };
    }
  } else {
    if (dowField !== '*') return advanced;
    const dayOfMonth = plainValue(domField, DOM);
    if (dayOfMonth === null) return advanced;
    days = { kind: 'monthly', dayOfMonth };
  }

  const time = parseTimeFields(minuteField, hourField);
  if (time === null) return advanced;

  return { time, days, timezone: tz, raw: null };
}

export function toCron(config: ScheduleConfig): string {
  const fields = cronFields(config);
  if (hasTimezonePrefix(fields)) return fields;
  if (fields === '' || fields !== fields.trim()) return fields;
  if (config.timezone === '' || config.timezone.includes(' ')) return fields;
  return `TZ=${config.timezone} ${fields}`;
}

export function cronFields(config: ScheduleConfig): string {
  if (config.raw !== null) return config.raw;

  let dom = '*';
  let dow = '*';
  if (config.days.kind === 'monthly') {
    dom = String(config.days.dayOfMonth);
  } else if (config.days.kind === 'weekly') {
    dow = config.days.daysOfWeek.length > 0 ? dowFieldFromDays(config.days.daysOfWeek) : '1';
  }

  const { time } = config;
  let minuteField: string;
  let hourField: string;
  if (time.kind === 'at') {
    const at = timeParts(time.time);
    minuteField = String(at.minute);
    hourField = String(at.hour);
  } else if (time.unit === 'hours') {
    if (time.window === null) {
      minuteField = String(time.minute);
      hourField = time.interval === 1 ? '*' : `*/${time.interval}`;
    } else {
      const from = timeParts(time.window.from);
      const to = timeParts(time.window.to);
      minuteField = String(from.minute);
      hourField =
        time.interval === 1
          ? `${from.hour}-${to.hour}`
          : `${from.hour}-${to.hour}/${time.interval}`;
    }
  } else {
    minuteField = time.interval === 1 ? '*' : `*/${time.interval}`;
    if (time.window === null) {
      hourField = '*';
    } else {
      const from = timeParts(time.window.from);
      const to = timeParts(time.window.to);
      hourField = from.hour === to.hour ? String(from.hour) : `${from.hour}-${to.hour}`;
    }
  }
  return `${minuteField} ${hourField} ${dom} * ${dow}`;
}
