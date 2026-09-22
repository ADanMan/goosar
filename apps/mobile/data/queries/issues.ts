// Запросы задач: список по воркспейсу, детали задачи, лента. Ключи — в ./issue-keys.
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";
import { issueKeys } from "./issue-keys";

export { issueKeys } from "./issue-keys";

export const issueListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: issueKeys.list(wsId),
    queryFn: async ({ signal }) => {
      const res = await api.listIssues({}, { signal });
      return res.issues;
    },
    enabled: !!wsId,
  });

export const issueDetailOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: issueKeys.detail(wsId, id),
    queryFn: ({ signal }) => api.getIssue(id, { signal }),
    enabled: !!wsId && !!id,
  });

export const issueTimelineOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: issueKeys.timeline(wsId, id),
    queryFn: ({ signal }) => api.listTimeline(id, { signal }),
    enabled: !!wsId && !!id,
  });

export const issueActiveTasksOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: issueKeys.activeTasks(wsId, id),
    queryFn: ({ signal }) => api.listActiveTasksForIssue(id, { signal }),
    enabled: !!wsId && !!id,
  });

export const issueTasksOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: issueKeys.tasks(wsId, id),
    queryFn: ({ signal }) => api.listTasksByIssue(id, { signal }),
    enabled: !!wsId && !!id,
  });

export const issueAttachmentsOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: issueKeys.attachments(wsId, id),
    queryFn: ({ signal }) => api.listAttachments(id, { signal }),
    enabled: !!wsId && !!id,
  });
