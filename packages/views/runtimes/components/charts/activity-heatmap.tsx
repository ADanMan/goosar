import { useMemo } from 'react';
import type { RuntimeUsage } from '@goosar/core/types';
import { useCustomPricingStore } from '@goosar/core/runtimes/custom-pricing-store';
import { addDaysIso, estimateCost, todayIso, weekStartIso } from '../../utils';
import { useT } from '../../../i18n';

const HEATMAP_WEEKS = 26;
const CELL_SIZE = 16;
const CELL_GAP = 3;
function weekdayNames(lang: string): string[] {
  const fmt = new Intl.DateTimeFormat(lang, { weekday: 'short' });
  return Array.from({ length: 7 }, (_, i) => fmt.format(new Date(Date.UTC(2024, 0, 1 + i))));
}

function getHeatmapColor(level: number): string {
  if (level === 0) return 'var(--color-muted)';
  const opacities = ['20%', '45%', '70%', '100%'];
  return `color-mix(in oklch, var(--color-chart-1) ${opacities[level - 1]}, transparent)`;
}

function fmtMoney(n: number): string {
  if (n >= 100) return `$${n.toFixed(0)}`;
  return `$${n.toFixed(2)}`;
}

function fmtDate(iso: string, lang: string): string {
  return new Date(iso + 'T00:00:00').toLocaleString(lang, {
    month: 'short',
    day: 'numeric',
  });
}

interface Insights {
  busiestDay: { date: string; cost: number } | null;
  busyDayName: string | null;
  busyDayAvg: number;
  quietDayName: string | null;
  quietDayAvg: number;
  totalCost: number;
  windowDays: number;
}

export function ActivityHeatmap({ usage, tz }: { usage: RuntimeUsage[]; tz: string }) {
  const { t, i18n } = useT('runtimes');
  const lang = i18n.language;
  const WEEKDAY_NAMES = useMemo(() => weekdayNames(lang), [lang]);
  const DAY_LABELS = useMemo(
    () => [WEEKDAY_NAMES[0] ?? '', '', WEEKDAY_NAMES[2] ?? '', '', WEEKDAY_NAMES[4] ?? '', '', ''],
    [WEEKDAY_NAMES],
  );
  const pricings = useCustomPricingStore((s) => s.pricings);
  const { cells, monthLabels, insights } = useMemo(() => {
    const dateCost = new Map<string, number>();
    for (const u of usage) {
      dateCost.set(u.date, (dateCost.get(u.date) ?? 0) + estimateCost(u));
    }

    const today = todayIso(tz);
    const lastWeekStart = weekStartIso(today);
    const startDate = addDaysIso(lastWeekStart, -(HEATMAP_WEEKS - 1) * 7);
    const todayIndex =
      (HEATMAP_WEEKS - 1) * 7 +
      (() => {
        const [y, m, d] = today.split('-').map(Number);
        const dt = new Date(Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1));
        return (dt.getUTCDay() + 6) % 7;
      })();

    const allCells: {
      date: string;
      dayOfWeek: number; 
      week: number;
      cost: number;
    }[] = [];
    for (let i = 0; i <= todayIndex; i++) {
      const dateStr = addDaysIso(startDate, i);
      const dayOfWeek = i % 7;
      const week = Math.floor(i / 7);
      allCells.push({
        date: dateStr,
        dayOfWeek,
        week,
        cost: dateCost.get(dateStr) ?? 0,
      });
    }

    const nonZero = allCells.filter((c) => c.cost > 0).map((c) => c.cost);
    nonZero.sort((a, b) => a - b);
    const getLevel = (cost: number) => {
      if (cost === 0) return 0;
      if (nonZero.length <= 1) return 4;
      const p = nonZero.indexOf(cost) / (nonZero.length - 1);
      if (p <= 0.25) return 1;
      if (p <= 0.5) return 2;
      if (p <= 0.75) return 3;
      return 4;
    };

    const cellsWithLevel = allCells.map((c) => ({
      ...c,
      level: getLevel(c.cost),
    }));

    const months: { label: string; week: number }[] = [];
    let lastMonth = -1;
    for (const c of cellsWithLevel) {
      const month = new Date(c.date + 'T00:00:00').getMonth();
      if (month !== lastMonth && c.dayOfWeek === 0) {
        months.push({
          label: new Date(c.date + 'T00:00:00').toLocaleString(lang, {
            month: 'short',
          }),
          week: c.week,
        });
        lastMonth = month;
      }
    }

    let busiestDay: { date: string; cost: number } | null = null;
    let totalCost = 0;
    const weekdaySum = [0, 0, 0, 0, 0, 0, 0];
    const weekdayCount = [0, 0, 0, 0, 0, 0, 0];
    for (const c of allCells) {
      totalCost += c.cost;
      weekdaySum[c.dayOfWeek] = (weekdaySum[c.dayOfWeek] ?? 0) + c.cost;
      weekdayCount[c.dayOfWeek] = (weekdayCount[c.dayOfWeek] ?? 0) + 1;
      if (c.cost > 0 && (!busiestDay || c.cost > busiestDay.cost)) {
        busiestDay = { date: c.date, cost: c.cost };
      }
    }
    const weekdayAvg = weekdaySum.map((s, i) => {
      const count = weekdayCount[i] ?? 0;
      return count > 0 ? s / count : 0;
    });
    let busyDayName: string | null = null;
    let busyDayAvg = 0;
    let quietDayName: string | null = null;
    let quietDayAvg = Number.POSITIVE_INFINITY;
    weekdayAvg.forEach((avg, i) => {
      const name = WEEKDAY_NAMES[i] ?? '';
      if (avg > busyDayAvg) {
        busyDayAvg = avg;
        busyDayName = name;
      }
      if (avg < quietDayAvg) {
        quietDayAvg = avg;
        quietDayName = name;
      }
    });
    if (quietDayAvg === Number.POSITIVE_INFINITY) quietDayAvg = 0;
    if (totalCost === 0) {
      busyDayName = null;
      quietDayName = null;
    }

    const insights: Insights = {
      busiestDay,
      busyDayName,
      busyDayAvg,
      quietDayName,
      quietDayAvg,
      totalCost,
      windowDays: allCells.length,
    };

    return { cells: cellsWithLevel, monthLabels: months, insights };
  }, [usage, pricings, tz, lang, WEEKDAY_NAMES]);

  const labelWidth = 28;
  const svgWidth = labelWidth + HEATMAP_WEEKS * (CELL_SIZE + CELL_GAP);
  const svgHeight = 14 + 7 * (CELL_SIZE + CELL_GAP);

  return (
    <div className="space-y-4">
      <div className="flex flex-col items-center gap-2">
        <div className="overflow-x-auto">
          <svg width={svgWidth} height={svgHeight} className="block">
            {monthLabels.map((m) => (
              <text
                key={`${m.label}-${m.week}`}
                x={labelWidth + m.week * (CELL_SIZE + CELL_GAP)}
                y={10}
                className="fill-muted-foreground"
                fontSize={9}
              >
                {m.label}
              </text>
            ))}
            {DAY_LABELS.map((label, i) =>
              label ? (
                <text
                  key={i}
                  x={0}
                  y={14 + i * (CELL_SIZE + CELL_GAP) + CELL_SIZE - 1}
                  className="fill-muted-foreground"
                  fontSize={9}
                >
                  {label}
                </text>
              ) : null,
            )}
            {cells.map((c) => (
              <rect
                key={c.date}
                x={labelWidth + c.week * (CELL_SIZE + CELL_GAP)}
                y={14 + c.dayOfWeek * (CELL_SIZE + CELL_GAP)}
                width={CELL_SIZE}
                height={CELL_SIZE}
                rx={3}
                fill={getHeatmapColor(c.level)}
                className="transition-colors"
              >
                <title>
                  {c.date}:{' '}
                  {c.cost > 0 ? `$${c.cost.toFixed(2)}` : t(($) => $.charts.heatmap_no_activity)}
                </title>
              </rect>
            ))}
          </svg>
        </div>
        <div className="flex items-center gap-1 text-[10px] text-muted-foreground">
          <span>{t(($) => $.charts.heatmap_less)}</span>
          {[0, 1, 2, 3, 4].map((level) => (
            <div
              key={level}
              className="h-[10px] w-[10px] rounded-[2px]"
              style={{ backgroundColor: getHeatmapColor(level) }}
            />
          ))}
          <span>{t(($) => $.charts.heatmap_more)}</span>
        </div>
      </div>

      <InsightsRow insights={insights} />
    </div>
  );
}

function InsightsRow({ insights }: { insights: Insights }) {
  const { t, i18n } = useT('runtimes');
  const { busiestDay, busyDayName, busyDayAvg, quietDayName, quietDayAvg, totalCost, windowDays } =
    insights;
  return (
    <dl className="grid grid-cols-2 gap-x-4 gap-y-3 border-t pt-3 sm:grid-cols-4">
      <Insight
        label={t(($) => $.charts.heatmap_busiest_day)}
        value={busiestDay ? fmtDate(busiestDay.date, i18n.language) : '—'}
        sub={busiestDay ? fmtMoney(busiestDay.cost) : null}
      />
      <Insight
        label={t(($) => $.charts.heatmap_most_active_weekday)}
        value={busyDayName ?? '—'}
        sub={busyDayName ? t(($) => $.charts.heatmap_avg, { value: fmtMoney(busyDayAvg) }) : null}
      />
      <Insight
        label={t(($) => $.charts.heatmap_quietest_weekday)}
        value={quietDayName ?? '—'}
        sub={quietDayName ? t(($) => $.charts.heatmap_avg, { value: fmtMoney(quietDayAvg) }) : null}
      />
      <Insight
        label={t(($) => $.charts.heatmap_window_total, { count: windowDays })}
        value={fmtMoney(totalCost)}
      />
    </dl>
  );
}

function Insight({ label, value, sub }: { label: string; value: string; sub?: string | null }) {
  return (
    <div className="min-w-0">
      <dt className="truncate text-[11px] uppercase tracking-wider text-muted-foreground">
        {label}
      </dt>
      <dd className="mt-0.5 truncate text-sm font-medium tabular-nums">
        {value}
        {sub != null && (
          <span className="ml-1.5 text-xs font-normal text-muted-foreground">{sub}</span>
        )}
      </dd>
    </div>
  );
}
