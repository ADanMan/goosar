// Realtime списка сессий чата (третий слой): монтируется глобально в layout
// воркспейса, держит кэш сессий свежим независимо от вкладки.
import { useQueryClient } from "@tanstack/react-query";
import { chatKeys } from "@/data/queries/chat";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
import {
  dropSessionFromList,
  patchSessionListAfterRename,
} from "./chat-ws-updaters";

export function useChatSessionsRealtime() {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      const invalidateSessions = () =>
        qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });

      return [
        ws.on("chat:done", invalidateSessions),
        ws.on("chat:session_read", invalidateSessions),
        ws.on("chat:session_updated", (p) => {
          const payload = p as {
            chat_session_id: string;
            title?: string;
            updated_at?: string;
          };
          patchSessionListAfterRename(qc, wsId, payload);
        }),
        ws.on("chat:session_deleted", (payload) => {
          dropSessionFromList(qc, wsId, payload);
        }),
        ws.onReconnect(invalidateSessions),
      ];
    },
    [qc],
  );
}
