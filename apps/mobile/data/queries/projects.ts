// Запросы проектов: список, детали и ресурсы проекта.
import { queryOptions } from "@tanstack/react-query";
import type { Project } from "@goosar/core/types";
import { api } from "@/data/api";
import { issueKeys } from "@/data/queries/issue-keys";

export const projectKeys = {
  all: (wsId: string | null) => ["projects", wsId] as const,
  list: (wsId: string | null) => [...projectKeys.all(wsId), "list"] as const,
  detail: (wsId: string | null, id: string) =>
    [...projectKeys.all(wsId), "detail", id] as const,
  resources: (wsId: string | null, id: string) =>
    [...projectKeys.all(wsId), "detail", id, "resources"] as const,
};

export const projectListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: projectKeys.list(wsId),
    queryFn: async ({ signal }) => {
      const res = await api.listProjects({ signal });
      return res.projects;
    },
    enabled: !!wsId,
  });

export const projectDetailOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: projectKeys.detail(wsId, id),
    queryFn: ({ signal }) => api.getProject(id, { signal }),
    enabled: !!wsId && !!id,
  });

export const projectResourcesOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: projectKeys.resources(wsId, id),
    queryFn: async ({ signal }) => {
      const res = await api.listProjectResources(id, { signal });
      return res.resources;
    },
    enabled: !!wsId && !!id,
  });

export const projectIssuesOptions = (wsId: string | null, projectId: string) =>
  queryOptions({
    queryKey: [
      ...issueKeys.list(wsId),
      "byProject",
      projectId,
    ] as const,
    queryFn: async ({ signal }) => {
      const res = await api.listIssues(
        { project_id: projectId },
        { signal },
      );
      return res.issues;
    },
    enabled: !!wsId && !!projectId,
  });

export function findProject(
  projects: Project[],
  id: string | null,
): Project | undefined {
  if (!id) return undefined;
  return projects.find((p) => p.id === id);
}
