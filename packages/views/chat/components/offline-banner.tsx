'use client';

import { AlertCircle, WifiOff } from 'lucide-react';
import type { AgentAvailability } from '@goosar/core/agents';
import { useT } from '../../i18n';

interface Props {
  agentName?: string;
  availability: AgentAvailability | undefined;
}

export function OfflineBanner({ agentName, availability }: Props) {
  const { t } = useT('chat');
  if (availability !== 'offline' && availability !== 'unstable') return null;

  const name = agentName?.trim() || t(($) => $.offline_banner.fallback_name);
  if (availability === 'unstable') {
    return (
      <div className="px-5 mb-1.5">
        <div className="mx-auto flex w-full max-w-4xl items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs bg-amber-50 dark:bg-amber-950/40 text-amber-900 dark:text-amber-200 ring-1 ring-amber-200/60 dark:ring-amber-900/40">
          <AlertCircle className="size-3.5 shrink-0" />
          <span className="truncate">{t(($) => $.offline_banner.unstable, { name })}</span>
        </div>
      </div>
    );
  }
  return (
    <div className="px-5 mb-1.5">
      <div className="mx-auto flex w-full max-w-4xl items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs bg-muted text-muted-foreground ring-1 ring-border">
        <WifiOff className="size-3.5 shrink-0" />
        <span className="truncate">{t(($) => $.offline_banner.offline, { name })}</span>
      </div>
    </div>
  );
}
