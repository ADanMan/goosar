'use client';

import { useEffect, useRef, useState } from 'react';
import { cn } from '@goosar/ui/lib/utils';
import { UnicodeSpinner } from '@goosar/ui/components/common/unicode-spinner';
import type { AgentAvailability } from '@goosar/core/agents';
import type { ChatPendingTask, TaskMessagePayload } from '@goosar/core/types';
import { formatElapsedSecs } from '../lib/format';
import { useT } from '../../i18n';

interface Props {
  pendingTask: ChatPendingTask;
  taskMessages: readonly TaskMessagePayload[];
  availability: AgentAvailability | undefined;
}

interface Stage {
  label: string;
  static?: boolean;
}

type StageKey =
  | 'offline'
  | 'reconnecting'
  | 'queued'
  | 'waiting_local_directory'
  | 'starting_up'
  | 'thinking'
  | 'typing';

type ToolKey =
  | 'running_command'
  | 'reading_files'
  | 'searching_code'
  | 'making_edits'
  | 'searching_web'
  | 'fallback';

const TOOL_KEY_BY_SLUG: Record<string, Exclude<ToolKey, 'fallback'>> = {
  bash: 'running_command',
  exec: 'running_command',
  read: 'reading_files',
  glob: 'reading_files',
  grep: 'searching_code',
  write: 'making_edits',
  edit: 'making_edits',
  multi_edit: 'making_edits',
  multiedit: 'making_edits',
  web_search: 'searching_web',
  websearch: 'searching_web',
};

export function pickStageKeys(
  status: string | undefined,
  taskMessages: readonly TaskMessagePayload[],
  availability: AgentAvailability | undefined,
): { stageKey: StageKey; toolKey?: ToolKey; static?: boolean } {
  if ((status === 'queued' || status === 'dispatched') && availability === 'offline') {
    return { stageKey: 'offline', static: true };
  }
  if ((status === 'queued' || status === 'dispatched') && availability === 'unstable') {
    return { stageKey: 'reconnecting' };
  }
  if (status === 'waiting_local_directory') {
    return { stageKey: 'waiting_local_directory', static: true };
  }
  if (status === 'queued') return { stageKey: 'queued' };
  if (status === 'dispatched') return { stageKey: 'starting_up' };

  let latest: TaskMessagePayload | null = null;
  for (let i = taskMessages.length - 1; i >= 0; i--) {
    const m = taskMessages[i];
    if (m && m.type !== 'error' && m.type !== 'tool_result') {
      latest = m;
      break;
    }
  }

  if (!latest) return { stageKey: 'thinking' };
  if (latest.type === 'thinking') return { stageKey: 'thinking' };
  if (latest.type === 'text') return { stageKey: 'typing' };
  if (latest.type === 'tool_use') {
    const tool = (latest.tool ?? '').toLowerCase();
    const toolKey = TOOL_KEY_BY_SLUG[tool] ?? 'fallback';
    return { stageKey: 'thinking', toolKey };
  }
  return { stageKey: 'thinking' };
}

function useResolveStage(): (
  status: string | undefined,
  taskMessages: readonly TaskMessagePayload[],
  availability: AgentAvailability | undefined,
) => Stage {
  const { t } = useT('chat');
  return (status, taskMessages, availability) => {
    const decision = pickStageKeys(status, taskMessages, availability);
    if (decision.toolKey) {
      return {
        label: t(($) => $.status_pill.tools[decision.toolKey!]),
      };
    }
    return {
      label: t(($) => $.status_pill.stages[decision.stageKey]),
      static: decision.static,
    };
  };
}

export function TaskStatusPill({ pendingTask, taskMessages, availability }: Props) {
  const resolveStage = useResolveStage();
  const anchorRef = useRef<number | null>(null);
  if (anchorRef.current === null) {
    if (pendingTask.created_at) {
      const t = Date.parse(pendingTask.created_at);
      anchorRef.current = Number.isFinite(t) ? t : Date.now();
    } else {
      anchorRef.current = Date.now();
    }
  }
  const anchor = anchorRef.current;

  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);

  const status = taskMessages.length > 0 ? 'running' : pendingTask.status;
  const elapsedSecs = Math.max(0, Math.floor((now - anchor) / 1000));
  const stage = resolveStage(status, taskMessages, availability);

  return (
    <div
      className="flex items-center gap-1.5 px-1 text-xs text-muted-foreground"
      aria-live="polite"
    >
      {!stage.static && <UnicodeSpinner name="breathe" className="opacity-70" />}
      <span className="truncate">
        <span className={cn(!stage.static && 'animate-chat-text-shimmer')}>{stage.label}</span>
        <span className="opacity-70"> · {formatElapsedSecs(elapsedSecs)}</span>
      </span>
    </div>
  );
}
