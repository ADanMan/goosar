// Realtime одной сессии чата (третий слой): монтируется экраном чата с id сессии,
// все обработчики фильтруют события по chat_session_id.
import { useQueryClient } from "@tanstack/react-query";
import { chatKeys } from "@/data/queries/chat";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
import {
  appendTaskMessage,
  applyChatDoneToCache,
  clearPendingTask,
  promotePendingTaskToRunning,
  seedPendingTaskFromQueued,
} from "./chat-ws-updaters";

export function useChatSessionRealtime(
  sessionId: string | null,
  onSessionDeleted?: () => void,
) {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws) => {
      if (!sessionId) return;

      const isMine = (p: { chat_session_id?: string }) =>
        p.chat_session_id === sessionId;

      const invalidateMine = () => {
        qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
        qc.invalidateQueries({ queryKey: chatKeys.pendingTask(sessionId) });
      };

      return [
        ws.on("chat:message", (payload) => {
          if (!isMine(payload)) return;
          qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
          qc.invalidateQueries({ queryKey: chatKeys.pendingTask(sessionId) });
        }),
        ws.on("chat:done", (payload) => {
          if (!isMine(payload)) return;
          applyChatDoneToCache(qc, payload);
        }),
        ws.on("task:queued", (payload) => {
          if (!isMine(payload)) return;
          seedPendingTaskFromQueued(qc, payload);
        }),
        ws.on("task:dispatch", (payload) => {
          if (!isMine(payload)) return;
          promotePendingTaskToRunning(qc, payload);
        }),
        ws.on("task:cancelled", (payload) => {
          if (!isMine(payload)) return;
          clearPendingTask(qc, sessionId);
        }),
        ws.on("task:completed", (payload) => {
          if (!isMine(payload)) return;
          clearPendingTask(qc, sessionId);
        }),
        ws.on("task:failed", (payload) => {
          if (!isMine(payload)) return;
          clearPendingTask(qc, sessionId);
          qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
        }),
        ws.on("chat:session_deleted", (payload) => {
          if (!isMine(payload)) return;
          onSessionDeleted?.();
        }),
        ws.on("task:message", (payload) => {
          if (!isMine(payload)) return;
          appendTaskMessage(qc, payload);
        }),
        ws.onReconnect(invalidateMine),
      ];
    },
    [sessionId, qc, onSessionDeleted],
  );
}
