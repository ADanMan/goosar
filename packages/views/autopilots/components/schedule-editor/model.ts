export type TimePattern =
  | { kind: 'at'; time: string } 
  | {
      kind: 'every';
      interval: number; 
      unit: 'minutes' | 'hours';
      window: { from: string; to: string } | null;
      minute: number;
    };

export type DayPattern =
  | { kind: 'every' }
  | { kind: 'weekly'; daysOfWeek: number[] } 
  | { kind: 'monthly'; dayOfMonth: number }; 

export interface ScheduleConfig {
  time: TimePattern;
  days: DayPattern;
  timezone: string; 
  raw: string | null;
}

export function getDefaultScheduleConfig(timezone: string): ScheduleConfig {
  return {
    time: { kind: 'at', time: '09:00' },
    days: { kind: 'every' },
    timezone,
    raw: null,
  };
}

export const DAY_KEYS = ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat'] as const;

export function consecutiveRuns(days: number[]): Array<[number, number]> {
  const sorted = Array.from(new Set(days)).toSorted((a, b) => a - b);
  const runs: Array<[number, number]> = [];
  let i = 0;
  while (i < sorted.length) {
    let j = i;
    while (j + 1 < sorted.length && sorted[j + 1] === sorted[j]! + 1) j++;
    runs.push([sorted[i]!, sorted[j]!]);
    i = j + 1;
  }
  return runs;
}

export function pad2(n: number): string {
  return String(n).padStart(2, '0');
}

export function timeParts(time: string): { hour: number; minute: number } {
  const [h, m] = time.split(':');
  return { hour: parseInt(h ?? '0', 10), minute: parseInt(m ?? '0', 10) };
}
