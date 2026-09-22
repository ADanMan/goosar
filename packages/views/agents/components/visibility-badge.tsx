'use client';

import { Globe, Lock } from 'lucide-react';
import type { AgentVisibility } from '@goosar/core/types';
import { Tooltip, TooltipTrigger, TooltipContent } from '@goosar/ui/components/ui/tooltip';
import { useT } from '../../i18n';

export function VisibilityBadge({
  value,
  compact = false,
  className = '',
}: {
  value: AgentVisibility;
  compact?: boolean;
  className?: string;
}) {
  const { t } = useT('agents');
  const Icon = value === 'private' ? Lock : Globe;
  const label = t(($) => $.visibility[value].label);
  const tooltip = t(($) => $.visibility[value].tooltip);

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span
            className={`inline-flex items-center gap-1 text-xs text-muted-foreground ${className}`}
            aria-label={tooltip}
          >
            <Icon className="h-3 w-3 shrink-0" />
            {!compact && <span className="truncate">{label}</span>}
          </span>
        }
      />
      <TooltipContent>{tooltip}</TooltipContent>
    </Tooltip>
  );
}
