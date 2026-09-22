import { BarChart, Bar, XAxis, YAxis, CartesianGrid } from 'recharts';
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from '@goosar/ui/components/ui/chart';
import { formatTokens, type DailyTokenData } from '../../utils';
import { useT, useUiLocale } from '../../../i18n';

export const tokenStackConfig = {
  input: { label: 'Input', color: 'var(--chart-1)' },
  output: { label: 'Output', color: 'var(--chart-2)' },
  cacheRead: { label: 'Cache read', color: 'var(--chart-4)' },
  cacheWrite: { label: 'Cache write', color: 'var(--chart-3)' },
} satisfies ChartConfig;

export function DailyTokensChart({ data }: { data: DailyTokenData[] }) {
  const { t } = useT('runtimes');
  const uiLocale = useUiLocale();
  return (
    <ChartContainer config={tokenStackConfig} className="aspect-[3/1] w-full">
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
              formatter={(value, name) =>
                typeof value === 'number' ? `${formatTokens(value)} ${name}` : `${value} ${name}`
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
        {/* Legend is rendered by the parent in the chart card header. */}
        <Bar dataKey="input" stackId="tokens" fill="var(--color-input)" radius={[0, 0, 0, 0]} />
        <Bar dataKey="output" stackId="tokens" fill="var(--color-output)" radius={[0, 0, 0, 0]} />
        <Bar
          dataKey="cacheRead"
          stackId="tokens"
          fill="var(--color-cacheRead)"
          radius={[0, 0, 0, 0]}
        />
        <Bar
          dataKey="cacheWrite"
          stackId="tokens"
          fill="var(--color-cacheWrite)"
          radius={[3, 3, 0, 0]}
        />
      </BarChart>
    </ChartContainer>
  );
}
