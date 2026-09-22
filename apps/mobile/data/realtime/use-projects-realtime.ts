// Realtime списка проектов: глобальная подписка на время сессии.
import { useQueryClient } from "@tanstack/react-query";
import { projectKeys } from "@/data/queries/projects";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
import {
  clearProjectDetail,
  patchProjectDetail,
  patchProjectsList,
  removeFromProjectsList,
  upsertIntoProjectsList,
} from "./project-ws-updaters";

export function useProjectsRealtime() {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      const invalidateList = () =>
        qc.invalidateQueries({ queryKey: projectKeys.list(wsId) });

      return [
        ws.on("project:created", (payload) => {
          upsertIntoProjectsList(qc, wsId, payload.project);
        }),
        ws.on("project:updated", (payload) => {
          patchProjectsList(qc, wsId, payload.project);
          patchProjectDetail(qc, wsId, payload.project);
        }),
        ws.on("project:deleted", (payload) => {
          removeFromProjectsList(qc, wsId, payload.project_id);
          clearProjectDetail(qc, wsId, payload.project_id);
        }),
        ws.onReconnect(invalidateList),
      ];
    },
    [qc],
  );
}
