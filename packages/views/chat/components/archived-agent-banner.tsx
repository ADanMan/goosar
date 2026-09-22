'use client';

import { Archive } from 'lucide-react';
import { useT } from '../../i18n';

export function ArchivedAgentBanner({ agentName }: { agentName?: string }) {
  const { t } = useT('chat');
  const name = agentName?.trim() || t(($) => $.offline_banner.fallback_name);
  return (
    <div className="px-5 mb-1.5">
      <div className="mx-auto flex w-full max-w-4xl items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs bg-muted text-muted-foreground ring-1 ring-border">
        <Archive className="size-3.5 shrink-0" />
        <span className="truncate">{t(($) => $.archived_agent_banner, { name })}</span>
      </div>
    </div>
  );
}
