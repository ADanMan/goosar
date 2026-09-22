'use client';

import { useEffect, useState } from 'react';
import { Button } from '@goosar/ui/components/ui/button';
import { Input } from '@goosar/ui/components/ui/input';
import { PropertyPicker } from '../../../issues/components/pickers';
import { CHIP_CLASS } from './chip';
import { useT } from '../../../i18n';

const MIN = 1;
const MAX = 50;

export function ConcurrencyPicker({
  value,
  canEdit = true,
  onChange,
}: {
  value: number;
  canEdit?: boolean;
  onChange: (next: number) => Promise<void> | void;
}) {
  const { t } = useT('agents');
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState(String(value));

  useEffect(() => {
    if (open) setDraft(String(value));
  }, [open, value]);

  if (!canEdit) {
    return <span className="font-mono text-xs tabular-nums text-muted-foreground">{value}</span>;
  }

  const commit = async () => {
    const n = Number(draft);
    if (!Number.isFinite(n) || n < MIN || n > MAX) return;
    setOpen(false);
    if (n !== value) await onChange(n);
  };

  const tooltip = t(($) => $.pickers.concurrency_tooltip, { value });

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-auto"
      align="start"
      tooltip={tooltip}
      triggerRender={<button type="button" className={CHIP_CLASS} aria-label={tooltip} />}
      trigger={<span className="font-mono tabular-nums">{value}</span>}
    >
      <div className="space-y-2 p-2">
        <p className="text-xs text-muted-foreground">
          {t(($) => $.pickers.concurrency_range, { min: MIN, max: MAX })}
        </p>
        <div className="flex items-center gap-2">
          <Input
            type="number"
            min={MIN}
            max={MAX}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                void commit();
              }
            }}
            autoFocus
            className="h-8 w-20 font-mono text-xs"
          />
          <Button size="sm" onClick={() => void commit()}>
            {t(($) => $.inspector.save)}
          </Button>
        </div>
      </div>
    </PropertyPicker>
  );
}
