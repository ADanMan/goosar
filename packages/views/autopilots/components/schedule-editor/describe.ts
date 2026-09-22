import { useCallback } from 'react';
import { useT } from '../../../i18n';
import type { ScheduleConfig } from './model';
import { consecutiveRuns, DAY_KEYS, pad2 } from './model';

type AutopilotsT = ReturnType<typeof useT<'autopilots'>>['t'];

function formatDayList(t: AutopilotsT, days: number[]): string {
  const name = (d: number) => t(($) => $.schedule_editor.describe.days_long[DAY_KEYS[d]!]);
  const parts: string[] = [];
  for (const [lo, hi] of consecutiveRuns(days)) {
    if (hi - lo >= 2) {
      parts.push(t(($) => $.schedule_editor.describe.days_range, { from: name(lo), to: name(hi) }));
    } else {
      for (let d = lo; d <= hi; d++) parts.push(name(d));
    }
  }
  return parts.join(t(($) => $.schedule_editor.describe.days_join));
}

export function describeSchedule(t: AutopilotsT, config: ScheduleConfig): string | null {
  if (config.raw !== null) return null;

  const clauses: string[] = [];
  const { time } = config;
  if (time.kind === 'at') {
    clauses.push(t(($) => $.schedule_editor.describe.time_at, { time: time.time }));
  } else if (time.unit === 'hours') {
    if (time.window === null) {
      const minute = pad2(time.minute);
      clauses.push(
        time.interval === 1
          ? t(($) => $.schedule_editor.describe.time_every_hour, { minute })
          : t(($) => $.schedule_editor.describe.time_every_hours, {
              interval: time.interval,
              minute,
            }),
      );
    } else {
      clauses.push(
        time.interval === 1
          ? t(($) => $.schedule_editor.describe.time_every_hour_window)
          : t(($) => $.schedule_editor.describe.time_every_hours_window, {
              interval: time.interval,
            }),
      );
      clauses.push(
        t(($) => $.schedule_editor.describe.window, {
          from: time.window.from,
          to: time.window.to,
        }),
      );
    }
  } else {
    clauses.push(
      time.interval === 1
        ? t(($) => $.schedule_editor.describe.time_every_minute)
        : t(($) => $.schedule_editor.describe.time_every_minutes, { interval: time.interval }),
    );
    if (time.window !== null) {
      clauses.push(
        t(($) => $.schedule_editor.describe.window, {
          from: time.window.from,
          to: time.window.to,
        }),
      );
    }
  }

  switch (config.days.kind) {
    case 'every':
      clauses.push(t(($) => $.schedule_editor.describe.days_every));
      break;
    case 'weekly':
      clauses.push(formatDayList(t, config.days.daysOfWeek));
      break;
    case 'monthly':
      clauses.push(
        t(($) => $.schedule_editor.describe.days_monthly, { day: config.days.dayOfMonth }),
      );
      break;
  }

  return clauses.join(' · ');
}

export function useDescribeSchedule(): (config: ScheduleConfig) => string | null {
  const { t } = useT('autopilots');
  return useCallback((config: ScheduleConfig) => describeSchedule(t, config), [t]);
}
