import { BarChart, Bar, XAxis, YAxis, CartesianGrid } from 'recharts';
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from '@goosar/ui/components/ui/chart';
import type { DailyCostStackData } from '../../utils';
import { useT } from '../../../i18n';
import { labelOf } from './failure-class-visuals';

export function useCostStackConfig(): ChartConfig {
  const { t } = useT('runtimes');
  return {
    input: { label: t(($) => $.usage.legend_input), color: 'var(--chart-1)' },
    output: { label: t(($) => $.usage.legend_output), color: 'var(--chart-2)' },
    cacheWrite: { label: t(($) => $.usage.legend_cache_write), color: 'var(--chart-3)' },
  } satisfies ChartConfig;
}

export function DailyCostChart({ data }: { data: DailyCostStackData[] }) {
  const { t } = useT('runtimes');
  const costStackConfig = useCostStackConfig();
  return (
    <ChartContainer config={costStackConfig} className="aspect-[3/1] w-full">
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
          tickFormatter={(v: number) => `$${v}`}
          width={50}
        />
        <ChartTooltip
          content={
            <ChartTooltipContent
              formatter={(value, name) =>
                typeof value === 'number'
                  ? `$${value.toFixed(2)} ${labelOf(costStackConfig, name)}`
                  : `${value} ${labelOf(costStackConfig, name)}`
              }
              footer={(payload) => {
                const total = payload.reduce(
                  (sum, item) => sum + (typeof item.value === 'number' ? item.value : 0),
                  0,
                );
                return (
                  <div className="flex items-center justify-between gap-2 font-medium">
                    <span>{t(($) => $.charts.tooltip_total)}</span>
                    <span className="font-mono tabular-nums">${total.toFixed(2)}</span>
                  </div>
                );
              }}
            />
          }
        />
        {/* Legend is intentionally rendered by the parent (in the chart card
            header, top-right) so the chart body stays clean and gets the full
            vertical real estate. */}
        <Bar dataKey="input" stackId="cost" fill="var(--color-input)" radius={[0, 0, 0, 0]} />
        <Bar dataKey="output" stackId="cost" fill="var(--color-output)" radius={[0, 0, 0, 0]} />
        <Bar
          dataKey="cacheWrite"
          stackId="cost"
          fill="var(--color-cacheWrite)"
          radius={[3, 3, 0, 0]}
        />
      </BarChart>
    </ChartContainer>
  );
}
