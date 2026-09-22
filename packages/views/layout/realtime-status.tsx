'use client';

import { ZapOff } from 'lucide-react';
import { Popover, PopoverContent, PopoverTrigger } from '@goosar/ui/components/ui/popover';
import { useRealtimeConnectionState } from '@goosar/core/realtime';
import { useT } from '../i18n';

export function RealtimeStatusIndicator() {
  const { t } = useT('layout');
  const state = useRealtimeConnectionState();

  if (state !== 'degraded') return null;

  const trigger = t(($) => $.realtime.degraded.title);

  return (
    <div className="pointer-events-none fixed bottom-3 left-3 z-40 flex">
      <Popover>
        <PopoverTrigger
          data-testid="realtime-status-indicator"
          aria-label={trigger}
          className="pointer-events-auto inline-flex size-7 items-center justify-center rounded-full border border-border bg-background text-warning shadow-sm transition-colors cursor-pointer hover:bg-accent hover:text-warning data-popup-open:bg-accent"
        >
          <ZapOff className="size-4" />
        </PopoverTrigger>
        <PopoverContent side="top" align="start" className="w-80 gap-2">
          <p className="text-sm font-medium">{t(($) => $.realtime.degraded.title)}</p>
          <p className="text-xs text-muted-foreground">{t(($) => $.realtime.degraded.detail)}</p>
          <p className="text-xs text-muted-foreground border-t border-border pt-2">
            {t(($) => $.realtime.degraded.cause)}
          </p>
        </PopoverContent>
      </Popover>
    </div>
  );
}
