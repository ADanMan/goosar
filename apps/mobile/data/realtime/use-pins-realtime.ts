// Realtime закреплений: payload событий pin:* не типизирован, поэтому
// инвалидация вместо патча.
import { useQueryClient } from "@tanstack/react-query";
import { pinKeys } from "@/data/queries/pins";
import { useAuthStore } from "@/data/auth-store";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";

export function usePinsRealtime() {
  const qc = useQueryClient();
  const userId = useAuthStore((s) => s.user?.id ?? null);

  useWSSubscriptions(
    (ws, wsId) => {
      if (!userId) return undefined;
      const invalidate = () =>
        qc.invalidateQueries({ queryKey: pinKeys.list(wsId, userId) });

      return [
        ws.on("pin:created", invalidate),
        ws.on("pin:deleted", invalidate),
        ws.on("pin:reordered", invalidate),
        ws.onReconnect(invalidate),
      ];
    },
    [qc, userId],
  );
}
