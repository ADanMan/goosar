'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Loader2, ScrollText } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { cn } from '@goosar/ui/lib/utils';
import { Tooltip, TooltipContent, TooltipTrigger } from '@goosar/ui/components/ui/tooltip';
import { api } from '@goosar/core/api';
import { isTaskMessageTaskId, taskMessagesOptions } from '@goosar/core/chat/queries';
import { useTaskMessageTail } from '@goosar/core/chat/use-task-message-tail';
import type { AgentTask } from '@goosar/core/types/agent';
import { AgentTranscriptDialog } from './agent-transcript-dialog';
import { buildTimeline, type TimelineItem } from './build-timeline';

interface TranscriptButtonProps {
  task: AgentTask;
  agentName: string;
  items?: TimelineItem[];
  isLive?: boolean;
  className?: string;
  title?: string;
  headerSlot?: React.ReactNode;
}

export function TranscriptButton({
  task,
  agentName,
  items: providedItems,
  isLive = false,
  className,
  title = 'View transcript',
  headerSlot,
}: TranscriptButtonProps) {
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [loadedItems, setLoadedItems] = useState<TimelineItem[] | null>(null);

  const liveCacheMode = isLive && providedItems === undefined && isTaskMessageTaskId(task.id);

  const [liveSession, setLiveSession] = useState(false);
  useEffect(() => {
    if (!open) setLiveSession(false);
  }, [open]);

  const items = providedItems ?? loadedItems ?? [];

  const handleClick = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      e.stopPropagation();
      if (liveCacheMode) {
        setLiveSession(true);
        setOpen(true);
        return;
      }
      if (providedItems !== undefined || loadedItems !== null) {
        setOpen(true);
        return;
      }
      setLoading(true);
      api
        .listTaskMessages(task.id)
        .then((msgs) => {
          setLoadedItems(buildTimeline(msgs));
          setOpen(true);
        })
        .catch((err) => {
          console.error(err);
          setLoadedItems([]);
          setOpen(true);
        })
        .finally(() => setLoading(false));
    },
    [liveCacheMode, providedItems, loadedItems, task.id],
  );

  useEffect(() => {
    if (!open) return;

    const handleGlobalNavigate = () => {
      setOpen(false);
    };

    window.addEventListener('goosar:navigate', handleGlobalNavigate);
    return () => {
      window.removeEventListener('goosar:navigate', handleGlobalNavigate);
    };
  }, [open]);

  return (
    <>
      <Tooltip>
        <TooltipTrigger
          render={<button type="button" />}
          onClick={handleClick}
          disabled={loading}
          aria-label={title}
          className={cn(
            'flex items-center justify-center rounded p-1 text-muted-foreground hover:text-foreground hover:bg-accent/50 transition-colors disabled:opacity-50',
            className,
          )}
        >
          {loading ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <ScrollText className="h-3.5 w-3.5" />
          )}
        </TooltipTrigger>
        <TooltipContent>{title}</TooltipContent>
      </Tooltip>

      {open &&
        (liveSession ? (
          <LiveTranscriptDialog
            task={task}
            agentName={agentName}
            isLive={isLive}
            onOpenChange={setOpen}
            headerSlot={headerSlot}
          />
        ) : (
          <AgentTranscriptDialog
            open={open}
            onOpenChange={setOpen}
            task={task}
            items={items}
            agentName={agentName}
            isLive={isLive}
            headerSlot={headerSlot}
          />
        ))}
    </>
  );
}

interface LiveTranscriptDialogProps {
  task: AgentTask;
  agentName: string;
  isLive: boolean;
  onOpenChange: (open: boolean) => void;
  headerSlot?: React.ReactNode;
}

function LiveTranscriptDialog({
  task,
  agentName,
  isLive,
  onOpenChange,
  headerSlot,
}: LiveTranscriptDialogProps) {
  const { data } = useQuery({
    ...taskMessagesOptions(task.id),
    enabled: false,
  });

  const backfill = useTaskMessageTail(task.id, isLive === true);

  useEffect(() => {
    backfill();
  }, [backfill, isLive]);

  const items = useMemo(() => buildTimeline(data ?? []), [data]);

  return (
    <AgentTranscriptDialog
      open
      onOpenChange={onOpenChange}
      task={task}
      items={items}
      agentName={agentName}
      isLive={isLive}
      headerSlot={headerSlot}
    />
  );
}
