// Realtime «Моих задач»: глобальная подписка на время сессии.
import { useQueryClient } from "@tanstack/react-query";
import { issueKeys } from "@/data/queries/issue-keys";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
import {
  patchIssueLabels,
  patchMyIssuesList,
  removeFromMyIssuesList,
} from "./issue-ws-updaters";

export function useMyIssuesRealtime() {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      const invalidateMyAll = () =>
        qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });

      return [
        ws.on("issue:created", () => invalidateMyAll()),
        ws.on("issue:updated", (payload) => {
          patchMyIssuesList(qc, wsId, payload.issue);
        }),
        ws.on("issue:deleted", (payload) => {
          removeFromMyIssuesList(qc, wsId, payload.issue_id);
        }),
        ws.on("issue_labels:changed", (payload) => {
          patchIssueLabels(qc, wsId, payload.issue_id, payload.labels);
        }),
        ws.onReconnect(invalidateMyAll),
      ];
    },
    [qc],
  );
}
