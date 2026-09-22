// Realtime входящих (третий слой): события inbox:* инвалидируют запрос,
// события задач патчат строки входящих.
import { useQueryClient } from "@tanstack/react-query";
import { inboxKeys } from "@/data/queries/inbox";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
import {
  dropInboxItemsByIssue,
  patchInboxIssueStatus,
} from "./inbox-ws-updaters";

export function useInboxRealtime() {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      const invalidate = () =>
        qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });

      return [
        ws.on("inbox:new", invalidate),
        ws.on("inbox:read", invalidate),
        ws.on("inbox:archived", invalidate),
        ws.on("inbox:unarchived", invalidate),
        ws.on("inbox:batch-read", invalidate),
        ws.on("inbox:batch-archived", invalidate),

        ws.on("issue:updated", (payload) => {
          patchInboxIssueStatus(
            qc,
            wsId,
            payload.issue.id,
            payload.issue.status,
          );
        }),
        ws.on("issue:deleted", (payload) => {
          dropInboxItemsByIssue(qc, wsId, payload.issue_id);
        }),

        ws.onReconnect(invalidate),
      ];
    },
    [qc],
  );
}
