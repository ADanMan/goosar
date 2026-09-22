// Realtime списка задач воркспейса: глобальная подписка на время сессии.
import { useQueryClient } from "@tanstack/react-query";
import { issueKeys } from "@/data/queries/issue-keys";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
import {
  patchIssuesList,
  prependToIssuesList,
  removeFromIssuesList,
} from "./issue-ws-updaters";

export function useIssuesRealtime() {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      const invalidateList = () =>
        qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });

      return [
        ws.on("issue:created", (payload) => {
          prependToIssuesList(qc, wsId, payload.issue);
        }),
        ws.on("issue:updated", (payload) => {
          patchIssuesList(qc, wsId, payload.issue);
        }),
        ws.on("issue:deleted", (payload) => {
          removeFromIssuesList(qc, wsId, payload.issue_id);
        }),
        ws.onReconnect(invalidateList),
      ];
    },
    [qc],
  );
}
