import { useCallback, useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { chatDraftRestoresOptions } from '@goosar/core/chat/queries';
import { useConsumeChatDraftRestore } from '@goosar/core/chat/mutations';
import { useChatStore } from '@goosar/core/chat';
import { removeChatMessageFromCaches } from '@goosar/core/realtime';
import type { Attachment } from '@goosar/core/types';

export interface RestoreDraftRequest {
  id: string;
  content: string;
  attachments?: Attachment[];
  sessionId?: string;
  serverRestoreId?: string;
}

export function useChatDraftRestore(activeSessionId: string | null, enabled = true) {
  const qc = useQueryClient();
  const [restoreDraftRequest, setRestoreDraftRequest] = useState<RestoreDraftRequest | null>(null);

  const appliedRestoreIds = useChatStore((s) => s.appliedDraftRestoreIds);
  const markDraftRestoreApplied = useChatStore((s) => s.markDraftRestoreApplied);
  const forgetDraftRestoreApplied = useChatStore((s) => s.forgetDraftRestoreApplied);
  const pendingSendRestores = useChatStore((s) => s.pendingSendRestores);
  const enqueuePendingSendRestore = useChatStore((s) => s.enqueuePendingSendRestore);
  const dequeuePendingSendRestore = useChatStore((s) => s.dequeuePendingSendRestore);
  const consumeDraftRestore = useConsumeChatDraftRestore();

  const { data: draftRestoresData } = useQuery({
    ...chatDraftRestoresOptions(activeSessionId ?? ''),
    enabled: enabled && !!activeSessionId,
  });
  const restores = draftRestoresData?.restores;

  const reconcilingRef = useRef<Set<string>>(new Set());
  const consume = useCallback(
    (sessionId: string, restoreId: string) => {
      reconcilingRef.current.add(restoreId);
      consumeDraftRestore.mutate(
        { sessionId, restoreId },
        {
          onSuccess: () => forgetDraftRestoreApplied(restoreId),
          onSettled: () => reconcilingRef.current.delete(restoreId),
        },
      );
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps -- mutation ref stable
    [forgetDraftRestoreApplied],
  );

  useEffect(() => {
    if (!enabled || !activeSessionId || !restores) return;
    for (const r of restores) {
      if (r.chat_session_id !== activeSessionId) continue;
      if (!appliedRestoreIds.includes(r.id)) continue;
      if (reconcilingRef.current.has(r.id)) continue;
      consume(activeSessionId, r.id);
    }
  }, [restores, activeSessionId, appliedRestoreIds, consume, enabled]);

  useEffect(() => {
    if (!enabled) {
      if (restoreDraftRequest) setRestoreDraftRequest(null);
      return;
    }
    if (!activeSessionId) return;
    if (restoreDraftRequest) {
      const { sessionId } = restoreDraftRequest;
      if (!sessionId || sessionId === activeSessionId) return;
      setRestoreDraftRequest(null);
      return;
    }
    const queued = pendingSendRestores[activeSessionId]?.[0];
    if (queued) {
      setRestoreDraftRequest({
        id: queued.id,
        content: queued.content,
        attachments: queued.attachments,
        sessionId: activeSessionId,
      });
      return;
    }
    const restore = restores?.find(
      (r) => r.chat_session_id === activeSessionId && !appliedRestoreIds.includes(r.id),
    );
    if (!restore) return;
    removeChatMessageFromCaches(qc, activeSessionId, restore.id);
    setRestoreDraftRequest({
      id: restore.id,
      content: restore.content ?? '',
      attachments: restore.attachments,
      sessionId: activeSessionId,
      serverRestoreId: restore.id,
    });
  }, [
    restores,
    activeSessionId,
    restoreDraftRequest,
    appliedRestoreIds,
    pendingSendRestores,
    qc,
    enabled,
  ]);

  const enqueueLocalRestore = useCallback(
    (restore: { id: string; content: string; attachments?: Attachment[]; sessionId: string }) => {
      enqueuePendingSendRestore(restore);
    },
    [enqueuePendingSendRestore],
  );

  const handleRestoreDraftApplied = useCallback(() => {
    if (!restoreDraftRequest) return;
    const { id, serverRestoreId, sessionId } = restoreDraftRequest;
    if (serverRestoreId && sessionId) {
      markDraftRestoreApplied(serverRestoreId);
      consume(sessionId, serverRestoreId);
    } else if (sessionId) {
      dequeuePendingSendRestore(sessionId, id);
    }
    setRestoreDraftRequest(null);
  }, [restoreDraftRequest, markDraftRestoreApplied, consume, dequeuePendingSendRestore]);

  return { restoreDraftRequest, enqueueLocalRestore, handleRestoreDraftApplied };
}
