// Централизованные ключи TanStack Query для задач на мобилке; форма префикса
// зеркалит веб, чтобы WS-инвалидация работала одинаково.
import type { ListIssuesParams } from "@goosar/core/types";

export type MyIssuesScope = "assigned" | "created" | "agents";

export type MyIssuesFilter = Pick<
  ListIssuesParams,
  "assignee_id" | "assignee_ids" | "creator_id" | "involves_user_id"
>;

export const issueKeys = {
  all: (wsId: string | null) => ["issues", wsId] as const,
  list: (wsId: string | null) => [...issueKeys.all(wsId), "list"] as const,
  myAll: (wsId: string | null) => [...issueKeys.all(wsId), "my"] as const,
  myList: (
    wsId: string | null,
    scope: MyIssuesScope,
    filter: MyIssuesFilter,
  ) => [...issueKeys.myAll(wsId), scope, filter] as const,
  detail: (wsId: string | null, id: string) =>
    [...issueKeys.all(wsId), "detail", id] as const,
  timeline: (wsId: string | null, id: string) =>
    [...issueKeys.all(wsId), "timeline", id] as const,
  activeTasks: (wsId: string | null, id: string) =>
    [...issueKeys.all(wsId), "active-tasks", id] as const,
  tasks: (wsId: string | null, id: string) =>
    [...issueKeys.all(wsId), "tasks", id] as const,
  attachments: (wsId: string | null, id: string) =>
    [...issueKeys.all(wsId), "attachments", id] as const,
};
