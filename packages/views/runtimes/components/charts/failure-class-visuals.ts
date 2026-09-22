import { FAILURE_CLASSES, type FailureClass } from '@goosar/core/dashboard';
import type { ChartConfig } from '@goosar/ui/components/ui/chart';
import { useT } from '../../../i18n';

export type FailureClassCounts = Record<FailureClass, number>;

export interface FailureBucketTotals {
  failed: number;
  total: number;
}

export const FAILURE_CLASS_COLOR: Record<FailureClass, string> = {
  auth: 'var(--destructive)',
  rate_limit: 'color-mix(in oklch, var(--destructive) 86%, var(--card))',
  timeout: 'color-mix(in oklch, var(--destructive) 72%, var(--card))',
  provider: 'color-mix(in oklch, var(--destructive) 60%, var(--card))',
  runtime: 'color-mix(in oklch, var(--destructive) 48%, var(--card))',
  agent: 'color-mix(in oklch, var(--destructive) 38%, var(--card))',
  other: 'color-mix(in oklch, var(--destructive) 30%, var(--card))',
};

export function activeFailureClasses(
  data: readonly Partial<Record<FailureClass, number>>[],
): FailureClass[] {
  return FAILURE_CLASSES.filter((c) => data.some((d) => (d[c] ?? 0) > 0));
}

export function formatRate(failed: number, total: number): string {
  if (total <= 0) return '—';
  const pct = (failed / total) * 100;
  return `${pct >= 10 || pct === 0 ? Math.round(pct) : pct.toFixed(1)}%`;
}

export function labelOf(config: ChartConfig, name: string | number | undefined): string {
  const key = String(name ?? '');
  const label = config[key]?.label;
  return typeof label === 'string' ? label : key;
}

export function useFailureClassConfig(): ChartConfig {
  const { t } = useT('usage');
  return {
    auth: { label: t(($) => $.errors.class.auth), color: FAILURE_CLASS_COLOR.auth },
    rate_limit: {
      label: t(($) => $.errors.class.rate_limit),
      color: FAILURE_CLASS_COLOR.rate_limit,
    },
    timeout: {
      label: t(($) => $.errors.class.timeout),
      color: FAILURE_CLASS_COLOR.timeout,
    },
    provider: {
      label: t(($) => $.errors.class.provider),
      color: FAILURE_CLASS_COLOR.provider,
    },
    runtime: {
      label: t(($) => $.errors.class.runtime),
      color: FAILURE_CLASS_COLOR.runtime,
    },
    agent: { label: t(($) => $.errors.class.agent), color: FAILURE_CLASS_COLOR.agent },
    other: { label: t(($) => $.errors.class.other), color: FAILURE_CLASS_COLOR.other },
  } satisfies ChartConfig;
}
