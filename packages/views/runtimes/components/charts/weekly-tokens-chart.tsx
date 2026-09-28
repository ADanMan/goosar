import { BarChart, Bar, XAxis, YAxis, CartesianGrid, Cell } from 'recharts';
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from '@goosar/ui/components/ui/chart';
import { formatTokens, type WeeklyTokenData } from '../../utils';
import { useT, useUiLocale } from '../../../i18n';
import { labelOf } from './failure-class-visuals';

export function useWeeklyTokenStackConfig(): ChartConfig {
  const { t } = useT('runtimes');
  return {
    input: { label: t(($) => $.usage.legend_input), color: 'var(--chart-1)' },
    output: { label: t(($) => $.usage.legend_output), color: 'var(--chart-2)' },
    cacheRead: { label: t(($) => $.usage.legend_cache_read), color: 'var(--chart-4)' },
    cacheWrite: { label: t(($) => $.usage.legend_cache_write), color: 'var(--chart-3)' },
  } satisfies ChartConfig;
}

export function WeeklyTokensChart({ data }: { data: WeeklyTokenData[] }) {
  const { t } = useT('runtimes');
  const uiLocale = useUiLocale();
  const weeklyTokenStackConfig = useWeeklyTokenStackConfig();
  return (
    <ChartContainer config={weeklyTokenStackConfig} className="aspect-[3/1] w-full">
      <BarChart data={data} margin={{ left: 0, right: 0, top: 4, bottom: 0 }}>
        <CartesianGrid vertical={false} />
        <XAxis
          dataKey="label"
          tickLine={false}
          axisLine={false}
          tickMargin={8}
          interval="preserveStartEnd"
        />
        <YAxis
          tickLine={false}
          axisLine={false}
          tickMargin={8}
          tickFormatter={(v: number) => formatTokens(v)}
          width={50}
        />
        <ChartTooltip
          content={
            <ChartTooltipContent
              labelKey="rangeLabel"
              labelFormatter={(_label, payload) => {
                const row = payload[0]?.payload as WeeklyTokenData | undefined;
                if (!row) return '';
                return row.partial
                  ? t(($) => $.usage.weekly_partial_label, {
                      range: row.rangeLabel,
                      covered: row.daysCovered,
                    })
                  : row.rangeLabel;
              }}
              formatter={(value, name) =>
                typeof value === 'number'
                  ? `${formatTokens(value)} ${labelOf(weeklyTokenStackConfig, name)}`
                  : `${value} ${labelOf(weeklyTokenStackConfig, name)}`
              }
              footer={(payload) => {
                const total = payload.reduce(
                  (sum, item) => sum + (typeof item.value === 'number' ? item.value : 0),
                  0,
                );
                return (
                  <div className="flex items-center justify-between gap-2 font-medium">
                    <span>{t(($) => $.charts.tooltip_total)}</span>
                    <span className="font-mono tabular-nums">{total.toLocaleString(uiLocale)}</span>
                  </div>
                );
              }}
            />
          }
        />
        <Bar dataKey="input" stackId="tokens" fill="var(--color-input)">
          {data.map((d) => (
            <Cell key={d.weekStart} fillOpacity={d.partial ? 0.5 : 1} />
          ))}
        </Bar>
        <Bar dataKey="output" stackId="tokens" fill="var(--color-output)">
          {data.map((d) => (
            <Cell key={d.weekStart} fillOpacity={d.partial ? 0.5 : 1} />
          ))}
        </Bar>
        <Bar dataKey="cacheRead" stackId="tokens" fill="var(--color-cacheRead)">
          {data.map((d) => (
            <Cell key={d.weekStart} fillOpacity={d.partial ? 0.5 : 1} />
          ))}
        </Bar>
        <Bar
          dataKey="cacheWrite"
          stackId="tokens"
          fill="var(--color-cacheWrite)"
          radius={[3, 3, 0, 0]}
        >
          {data.map((d) => (
            <Cell key={d.weekStart} fillOpacity={d.partial ? 0.5 : 1} />
          ))}
        </Bar>
      </BarChart>
    </ChartContainer>
  );
}
