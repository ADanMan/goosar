'use client';

import { useCallback, useEffect, useRef } from 'react';
import { focusManager, useQueryClient } from '@tanstack/react-query';

import { api } from '../api';
import { createLogger } from '../logger';
import { useRealtimePollingInterval } from '../realtime';
import type { TaskMessagePayload } from '../types/events';
import {
  chatKeys,
  highestTaskMessageSeq,
  isTaskMessageTaskId,
  mergeTaskMessagesBySeq,
} from './queries';

const logger = createLogger('chat.task-tail');

export function useTaskMessageTail(taskId: string, enabled: boolean): () => void {
  const queryClient = useQueryClient();
  const mountedRef = useRef(true);
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const healedRef = useRef(false);

  const backfill = useCallback(
    (mode: 'full' | 'tail' = 'full') => {
      if (!isTaskMessageTaskId(taskId)) return;
      const key = chatKeys.taskMessages(taskId);
      const since =
        mode === 'tail'
          ? highestTaskMessageSeq(queryClient.getQueryData<TaskMessagePayload[]>(key))
          : null;
      const request =
        since !== null ? api.listTaskMessages(taskId, { since }) : api.listTaskMessages(taskId);
      request
        .then((messages) => {
          if (!mountedRef.current) return;
          if (since === null) healedRef.current = true;
          queryClient.setQueryData<TaskMessagePayload[]>(key, (old = []) =>
            mergeTaskMessagesBySeq(old, messages),
          );
        })
        .catch((err) => {
          logger.error('task transcript backfill failed', err);
        });
    },
    [queryClient, taskId],
  );

  const pollingInterval = useRealtimePollingInterval();

  useEffect(() => {
    if (!enabled || pollingInterval === false) return;
    healedRef.current = false;
    const timer = setInterval(() => {
      if (!focusManager.isFocused()) return;
      backfill(healedRef.current ? 'tail' : 'full');
    }, pollingInterval);
    return () => clearInterval(timer);
  }, [backfill, enabled, pollingInterval]);

  return backfill;
}
