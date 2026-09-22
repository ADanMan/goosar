'use client';

import { useT } from '../../i18n';

export function CharCounter({ length, max }: { length: number; max: number }) {
  const { t } = useT('agents');
  const over = length > max;
  const near = !over && length >= Math.floor(max * 0.9);
  const tone = over ? 'text-destructive' : near ? 'text-warning' : 'text-muted-foreground';
  return (
    <div className={`text-right text-xs tabular-nums ${tone}`}>
      {length} / {max}
      {over && t(($) => $.char_counter.over_limit, { count: length - max })}
    </div>
  );
}
