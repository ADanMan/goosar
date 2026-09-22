'use client';

import type { TaskAttribution } from '@goosar/core/types';
import { Badge } from '@goosar/ui/components/ui/badge';
import { ActorAvatar } from '@goosar/ui/components/common/actor-avatar';
import { Tooltip, TooltipContent, TooltipTrigger } from '@goosar/ui/components/ui/tooltip';
import { cn } from '@goosar/ui/lib/utils';
import { useT } from '../../i18n';

function initialsOf(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  const first = parts[0];
  if (!first) return '?';
  const last = parts[parts.length - 1];
  if (parts.length === 1 || !last) return first.slice(0, 2).toUpperCase();
  return (first.charAt(0) + last.charAt(0)).toUpperCase();
}

export function AttributionBadge({
  attribution,
  className,
  variant = 'badge',
  hideAvatar = false,
}: {
  attribution?: TaskAttribution;
  className?: string;
  variant?: 'badge' | 'avatar' | 'inline';
  hideAvatar?: boolean;
}) {
  const { t } = useT('issues');
  if (!attribution) return null;

  let sourceLabel: string;
  switch (attribution.source) {
    case 'direct_human':
      sourceLabel = t(($) => $.execution_log.attribution.source_direct_human);
      break;
    case 'delegation':
      sourceLabel = t(($) => $.execution_log.attribution.source_delegation);
      break;
    case 'comment_source':
      sourceLabel = t(($) => $.execution_log.attribution.source_comment_source);
      break;
    case 'trigger_owner':
      sourceLabel = t(($) => $.execution_log.attribution.source_trigger_owner);
      break;
    case 'rule_owner':
      sourceLabel = t(($) => $.execution_log.attribution.source_rule_owner);
      break;
    case 'owner_fallback':
      sourceLabel = t(($) => $.execution_log.attribution.source_owner_fallback);
      break;
    case 'backfill':
      sourceLabel = t(($) => $.execution_log.attribution.source_backfill);
      break;
    case 'unattributed':
      sourceLabel = t(($) => $.execution_log.attribution.source_unattributed);
      break;
    default:
      sourceLabel = attribution.source;
  }

  const uncertain = attribution.precise === false && attribution.source !== 'backfill';
  const initiator = attribution.initiator;

  if (variant === 'inline') {
    const initiatorInline = attribution.initiator;
    if (!initiatorInline) return null;
    const name = initiatorInline.name || t(($) => $.execution_log.attribution.someone);
    return (
      <Tooltip>
        <TooltipTrigger
          render={
            <span
              className={cn(
                'inline-flex min-w-0 items-center gap-1.5 text-xs',
                uncertain ? 'text-warning' : 'text-foreground/80',
                className,
              )}
            >
              {!hideAvatar && (
                <ActorAvatar
                  name={name}
                  initials={initialsOf(name)}
                  avatarUrl={initiatorInline.avatar_url}
                  size="xs"
                  className="shrink-0"
                />
              )}
              <span className="min-w-0 truncate">{name}</span>
            </span>
          }
        />
        <TooltipContent>{sourceLabel}</TooltipContent>
      </Tooltip>
    );
  }

  if (variant === 'avatar') {
    if (!initiator) return null;
    const name = initiator.name || t(($) => $.execution_log.attribution.someone);
    return (
      <Tooltip>
        <TooltipTrigger
          render={
            <span
              className={cn(
                'inline-flex shrink-0',
                uncertain && 'rounded-full ring-1 ring-warning/60',
                className,
              )}
            >
              <ActorAvatar
                name={name}
                initials={initialsOf(name)}
                avatarUrl={initiator.avatar_url}
                size="xs"
              />
            </span>
          }
        />
        <TooltipContent>
          <div className="flex flex-col">
            <span>{t(($) => $.execution_log.attribution.on_behalf_of, { name })}</span>
            <span
              className={cn('text-[11px]', uncertain ? 'text-warning' : 'text-muted-foreground')}
            >
              {sourceLabel}
            </span>
          </div>
        </TooltipContent>
      </Tooltip>
    );
  }

  if (!initiator) return null;

  const name = initiator.name || t(($) => $.execution_log.attribution.someone);
  return (
    <Badge
      variant="outline"
      className={cn(
        'max-w-40 min-w-0 gap-1 font-normal',
        uncertain ? 'text-warning' : 'text-muted-foreground',
        className,
      )}
      title={sourceLabel}
    >
      <ActorAvatar
        name={name}
        initials={initialsOf(name)}
        avatarUrl={initiator.avatar_url}
        size="xs"
        className="shrink-0"
      />
      <span className="min-w-0 truncate">
        {t(($) => $.execution_log.attribution.on_behalf_of, { name })}
      </span>
    </Badge>
  );
}
