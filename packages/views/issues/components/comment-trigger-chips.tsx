'use client';

import { useMemo, useState } from 'react';
import { TriangleAlert } from 'lucide-react';
import type { CommentTriggerPreviewAgent, CommentTriggerOutcome } from '@goosar/core/types';
import { useAgentPresenceDetail } from '@goosar/core/agents';
import { mentionLabelsByTarget } from '@goosar/core/issues/comment-trigger-outcomes';
import { useCurrentWorkspace } from '@goosar/core/paths';
import { ActorAvatar as ActorAvatarBase } from '@goosar/ui/components/common/actor-avatar';
import { AVATAR_SIZE_PX } from '@goosar/ui/lib/avatar-size';
import { Popover, PopoverContent, PopoverTrigger } from '@goosar/ui/components/ui/popover';
import { Tooltip, TooltipContent, TooltipTrigger } from '@goosar/ui/components/ui/tooltip';
import { cn } from '@goosar/ui/lib/utils';
import { AgentStatusDot } from '../../common/actor-avatar';
import { useT } from '../../i18n';
import { blockedReasonLabel, blockedShortReasonLabel } from '../blocked-trigger-copy';

const AVATAR_SIZE = AVATAR_SIZE_PX.xs;
const MAX_STACK_HEADS = 4;

interface CommentTriggerChipsProps {
  agents: CommentTriggerPreviewAgent[];
  blocked?: CommentTriggerOutcome[];
  draftContent?: string;
  suppressedAgentIds: Set<string>;
  onToggle: (agentId: string) => void;
}

type IssuesT = ReturnType<typeof useT<'issues'>>['t'];

function sourceLabel(source: string, t: IssuesT): string {
  switch (source) {
    case 'issue_assignee':
      return t(($) => $.comment.trigger_source_issue_assignee);
    case 'mention_agent':
      return t(($) => $.comment.trigger_source_mention_agent);
    case 'mention_squad_leader':
      return t(($) => $.comment.trigger_source_mention_squad_leader);
    default:
      return t(($) => $.comment.trigger_source_unknown);
  }
}

function sourceReason(agent: CommentTriggerPreviewAgent, t: IssuesT): string | null {
  switch (agent.source) {
    case 'issue_assignee':
    case 'mention_agent':
      return null;
    case 'mention_squad_leader':
      return t(($) => $.comment.trigger_reason_mention_squad_leader);
    default:
      return agent.reason || t(($) => $.comment.trigger_reason_unknown);
  }
}

function useTriggerPresenceLine(agentId: string, t: IssuesT): string | null {
  const ws = useCurrentWorkspace();
  const detail = useAgentPresenceDetail(ws?.id, agentId);
  if (detail === 'loading') return null;
  return detail.availability === 'online' || detail.availability === 'unstable'
    ? t(($) => $.comment.trigger_starts_now)
    : t(($) => $.comment.trigger_starts_when_online);
}

function TriggerAgentTooltipBody({
  agent,
  suppressed,
  t,
}: {
  agent: CommentTriggerPreviewAgent;
  suppressed: boolean;
  t: IssuesT;
}) {
  const presenceLine = useTriggerPresenceLine(agent.id, t);
  return (
    <div className="space-y-0.5">
      <div className="flex items-baseline gap-1.5">
        <span className="font-medium">{agent.name}</span>
        <span className="text-[10px] text-muted-foreground">{sourceLabel(agent.source, t)}</span>
      </div>
      {suppressed ? (
        <div>{t(($) => $.comment.trigger_click_to_restore)}</div>
      ) : (
        <>
          {(() => {
            const line = [sourceReason(agent, t), presenceLine].filter(Boolean).join(' ');
            return line ? <div>{line}</div> : null;
          })()}
          <div className="text-muted-foreground">{t(($) => $.comment.trigger_click_to_skip)}</div>
        </>
      )}
    </div>
  );
}

export function CommentTriggerChips({
  agents,
  blocked = [],
  draftContent = '',
  suppressedAgentIds,
  onToggle,
}: CommentTriggerChipsProps) {
  const { t } = useT('issues');
  const blockedLabels = useMemo(() => mentionLabelsByTarget(draftContent), [draftContent]);

  if (agents.length === 0 && blocked.length === 0) return null;

  const allowed =
    agents.length === 1 ? (
      <SingleTriggerChip
        agent={agents[0]!}
        suppressed={suppressedAgentIds.has(agents[0]!.id)}
        onToggle={onToggle}
        t={t}
      />
    ) : agents.length > 1 ? (
      <MultiTriggerChip
        agents={agents}
        suppressedAgentIds={suppressedAgentIds}
        onToggle={onToggle}
        t={t}
      />
    ) : null;

  if (blocked.length === 0) return allowed;

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {allowed}
      {blocked.map((outcome) => (
        <BlockedTriggerChip
          key={`${outcome.target_type}:${outcome.target_id}`}
          outcome={outcome}
          label={blockedLabels.get(`${outcome.target_type}:${outcome.target_id}`)}
          t={t}
        />
      ))}
    </div>
  );
}

function BlockedTriggerChip({
  outcome,
  label,
  t,
}: {
  outcome: CommentTriggerOutcome;
  label?: string;
  t: IssuesT;
}) {
  const shortReason = blockedShortReasonLabel(outcome.reason_code, t);
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span
            className="inline-flex h-6 min-w-0 max-w-full animate-in fade-in items-center gap-1.5 rounded-md px-1.5 text-[11px] font-medium text-destructive"
            aria-label={
              label
                ? t(($) => $.comment.trigger_blocked_chip_aria, {
                    name: label,
                    reason: shortReason,
                  })
                : shortReason
            }
          >
            <TriangleAlert className="size-3 shrink-0" />
            {label ? (
              <span className="inline-flex min-w-0 items-center gap-1">
                <span className="truncate">{label}</span>
                <span className="shrink-0">·</span>
                <span className="shrink-0">{shortReason}</span>
              </span>
            ) : (
              <span className="truncate">{shortReason}</span>
            )}
          </span>
        }
      />
      <TooltipContent side="top" className="max-w-72 text-xs">
        {blockedReasonLabel(outcome.reason_code, t)}
      </TooltipContent>
    </Tooltip>
  );
}

function SingleTriggerChip({
  agent,
  suppressed,
  onToggle,
  t,
}: {
  agent: CommentTriggerPreviewAgent;
  suppressed: boolean;
  onToggle: (agentId: string) => void;
  t: IssuesT;
}) {
  const state = suppressed
    ? t(($) => $.comment.trigger_skipped_label)
    : sourceLabel(agent.source, t);
  const sentence = suppressed
    ? t(($) => $.comment.trigger_wont_trigger)
    : t(($) => $.comment.trigger_will_start);

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            aria-pressed={suppressed}
            aria-label={t(($) => $.comment.trigger_chip_aria, { name: agent.name, state })}
            onClick={() => onToggle(agent.id)}
            className={cn(
              'inline-flex h-6 min-w-0 max-w-full animate-in fade-in cursor-pointer items-center gap-1.5 rounded-md px-1.5 text-[11px] font-medium text-muted-foreground transition-colors duration-200 hover:bg-muted hover:text-foreground',
              suppressed && 'opacity-60',
            )}
          >
            <TriggerAgentAvatar agent={agent} suppressed={suppressed} />
            <span className="truncate">{sentence}</span>
          </button>
        }
      />
      <TooltipContent side="top" className="max-w-72 text-xs">
        <TriggerAgentTooltipBody agent={agent} suppressed={suppressed} t={t} />
      </TooltipContent>
    </Tooltip>
  );
}

function MultiTriggerChip({
  agents,
  suppressedAgentIds,
  onToggle,
  t,
}: {
  agents: CommentTriggerPreviewAgent[];
  suppressedAgentIds: Set<string>;
  onToggle: (agentId: string) => void;
  t: IssuesT;
}) {
  const [open, setOpen] = useState(false);
  const [tooltipHover, setTooltipHover] = useState(false);
  const activeCount = agents.filter((a) => !suppressedAgentIds.has(a.id)).length;
  const heads = agents.slice(0, MAX_STACK_HEADS);
  const overflow = agents.length - heads.length;
  const overlap = Math.round(AVATAR_SIZE * 0.3);
  const sentence =
    activeCount === 0
      ? t(($) => $.comment.trigger_none_will_trigger)
      : t(($) => $.comment.trigger_will_start_count, { count: activeCount });

  const popoverTrigger = (
    <PopoverTrigger
      render={
        <button
          type="button"
          className={cn(
            'inline-flex h-6 min-w-0 max-w-full animate-in fade-in cursor-pointer items-center gap-1.5 rounded-md px-1.5 text-[11px] font-medium text-muted-foreground transition-colors duration-200 hover:bg-muted hover:text-foreground aria-expanded:bg-muted aria-expanded:text-foreground',
            activeCount === 0 && 'opacity-60',
          )}
        />
      }
    >
      <span className="inline-flex items-center">
        {heads.map((agent, i) => (
          <span
            key={agent.id}
            style={{ marginLeft: i === 0 ? 0 : -overlap }}
            className="inline-flex rounded-full ring-2 ring-background"
          >
            <TriggerAgentAvatar
              agent={agent}
              suppressed={suppressedAgentIds.has(agent.id)}
              showDot={false}
            />
          </span>
        ))}
        {overflow > 0 && (
          <span
            style={{
              marginLeft: -overlap,
              width: AVATAR_SIZE,
              height: AVATAR_SIZE,
              fontSize: Math.max(9, Math.round(AVATAR_SIZE * 0.45)),
            }}
            className="inline-flex items-center justify-center rounded-full bg-muted font-medium tabular-nums text-muted-foreground ring-2 ring-background"
          >
            +{overflow}
          </span>
        )}
      </span>
      <span className="truncate">{sentence}</span>
    </PopoverTrigger>
  );

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <Tooltip open={tooltipHover && !open} onOpenChange={setTooltipHover}>
        <TooltipTrigger render={popoverTrigger} />
        <TooltipContent side="top" className="text-xs">
          {t(($) => $.comment.trigger_click_to_manage)}
        </TooltipContent>
      </Tooltip>
      <PopoverContent align="start" className="w-64 p-2">
        <div className="px-1.5 pb-1 text-xs font-medium text-muted-foreground">
          {t(($) => $.comment.trigger_preview_title)}
        </div>
        <div className="flex flex-col">
          {agents.map((agent) => {
            const suppressed = suppressedAgentIds.has(agent.id);
            const state = suppressed
              ? t(($) => $.comment.trigger_skipped_label)
              : sourceLabel(agent.source, t);
            return (
              <Tooltip key={agent.id}>
                <TooltipTrigger
                  render={
                    <button
                      type="button"
                      aria-pressed={suppressed}
                      aria-label={t(($) => $.comment.trigger_chip_aria, {
                        name: agent.name,
                        state,
                      })}
                      onClick={() => onToggle(agent.id)}
                      className={cn(
                        'flex w-full cursor-pointer items-center gap-2 rounded-md px-1.5 py-1 text-left transition-colors hover:bg-muted',
                        suppressed && 'opacity-60',
                      )}
                    >
                      <TriggerAgentAvatar agent={agent} suppressed={suppressed} />
                      <span
                        className={cn(
                          'min-w-0 flex-1 truncate text-xs',
                          suppressed && 'text-muted-foreground',
                        )}
                      >
                        {agent.name}
                      </span>
                      <span className="shrink-0 text-[10px] text-muted-foreground">{state}</span>
                    </button>
                  }
                />
                <TooltipContent side="right" className="max-w-72 text-xs">
                  <TriggerAgentTooltipBody agent={agent} suppressed={suppressed} t={t} />
                </TooltipContent>
              </Tooltip>
            );
          })}
        </div>
      </PopoverContent>
    </Popover>
  );
}

function TriggerAgentAvatar({
  agent,
  suppressed,
  showDot = true,
}: {
  agent: CommentTriggerPreviewAgent;
  suppressed: boolean;
  showDot?: boolean;
}) {
  return (
    <span className={cn('relative inline-flex shrink-0', suppressed && 'opacity-40 grayscale')}>
      <ActorAvatarBase
        name={agent.name}
        initials=""
        avatarUrl={agent.avatar_url}
        isAgent
        size="xs"
      />
      {showDot && !suppressed && <AgentStatusDot agentId={agent.id} size="xs" />}
    </span>
  );
}
