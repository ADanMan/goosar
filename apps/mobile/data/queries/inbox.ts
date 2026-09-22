import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const inboxKeys = {
  all: (wsId: string | null) => ["inbox", wsId] as const,
  list: (wsId: string | null) =>
    [...inboxKeys.all(wsId), "list"] as const,
};

export const inboxListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: inboxKeys.list(wsId),
    queryFn: ({ signal }) => api.listInbox({ signal }),
    enabled: !!wsId,
  });
