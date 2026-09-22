// Realtime одного проекта: монтируется экраном проекта, фильтрует события по id.
import { useQueryClient } from "@tanstack/react-query";
import type { Issue } from "@goosar/core/types";
import { issueKeys } from "@/data/queries/issue-keys";
import { projectKeys } from "@/data/queries/projects";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
import {
  clearProjectDetail,
  patchProjectDetail,
  removeFromProjectsList,
} from "./project-ws-updaters";

export function useProjectRealtime(
  projectId: string | undefined,
  onDeleted?: () => void,
) {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      if (!projectId) return;

      const issueListKey = [
        ...issueKeys.list(wsId),
        "byProject",
        projectId,
      ] as const;

      const invalidateThisProject = () => {
        qc.invalidateQueries({ queryKey: projectKeys.detail(wsId, projectId) });
        qc.invalidateQueries({
          queryKey: projectKeys.resources(wsId, projectId),
        });
        qc.invalidateQueries({ queryKey: issueListKey });
      };

      return [
        ws.on("project:updated", (payload) => {
          if (payload.project.id !== projectId) return;
          patchProjectDetail(qc, wsId, payload.project);
        }),
        ws.on("project:deleted", (payload) => {
          if (payload.project_id !== projectId) return;
          clearProjectDetail(qc, wsId, projectId);
          removeFromProjectsList(qc, wsId, projectId);
          onDeleted?.();
        }),

        ws.on("issue:updated", (payload) => {
          const issue = payload.issue;
          const wasInList = (
            qc.getQueryData<Issue[]>(issueListKey) ?? []
          ).some((i) => i.id === issue.id);
          const nowInProject = issue.project_id === projectId;
          if (!wasInList && !nowInProject) return;
          qc.setQueryData<Issue[]>(issueListKey, (old) => {
            if (!old) return old;
            if (nowInProject) {
              return old.some((i) => i.id === issue.id)
                ? old.map((i) => (i.id === issue.id ? issue : i))
                : [...old, issue];
            }
            return old.filter((i) => i.id !== issue.id);
          });
        }),
        ws.on("issue:created", (payload) => {
          if (payload.issue.project_id !== projectId) return;
          qc.invalidateQueries({ queryKey: issueListKey });
        }),
        ws.on("issue:deleted", (payload) => {
          qc.setQueryData<Issue[]>(issueListKey, (old) =>
            old ? old.filter((i) => i.id !== payload.issue_id) : old,
          );
        }),

        ws.onReconnect(invalidateThisProject),
      ];
    },
    [projectId, qc, onDeleted],
  );
}
