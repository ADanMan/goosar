// Патчеры WS-кэша проектов: чистые функции над QueryClient.
import type { QueryClient } from "@tanstack/react-query";
import type { Project } from "@goosar/core/types";
import { projectKeys } from "@/data/queries/projects";

export function patchProjectsList(
  qc: QueryClient,
  wsId: string,
  partial: Partial<Project> & { id: string },
) {
  qc.setQueryData<Project[]>(projectKeys.list(wsId), (old) =>
    old
      ? old.map((p) => (p.id === partial.id ? { ...p, ...partial } : p))
      : old,
  );
}

export function upsertIntoProjectsList(
  qc: QueryClient,
  wsId: string,
  project: Project,
) {
  qc.setQueryData<Project[]>(projectKeys.list(wsId), (old) => {
    if (!old) return [project];
    const idx = old.findIndex((p) => p.id === project.id);
    if (idx === -1) return [project, ...old];
    const copy = old.slice();
    copy[idx] = project;
    return copy;
  });
}

export function removeFromProjectsList(
  qc: QueryClient,
  wsId: string,
  projectId: string,
) {
  qc.setQueryData<Project[]>(projectKeys.list(wsId), (old) =>
    old ? old.filter((p) => p.id !== projectId) : old,
  );
}

export function patchProjectDetail(
  qc: QueryClient,
  wsId: string,
  project: Project,
) {
  qc.setQueryData<Project>(projectKeys.detail(wsId, project.id), project);
}

export function clearProjectDetail(
  qc: QueryClient,
  wsId: string,
  projectId: string,
) {
  qc.removeQueries({ queryKey: projectKeys.detail(wsId, projectId) });
  qc.removeQueries({ queryKey: projectKeys.resources(wsId, projectId) });
}
