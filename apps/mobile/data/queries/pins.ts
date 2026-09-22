// Фабрика ключей кэша закреплений: список per-user-per-workspace, поэтому
// в ключе есть и wsId, и userId.
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const pinKeys = {
  all: (wsId: string | null, userId: string | null) =>
    ["pins", wsId, userId] as const,
  list: (wsId: string | null, userId: string | null) =>
    [...pinKeys.all(wsId, userId), "list"] as const,
};

export const pinListOptions = (wsId: string | null, userId: string | null) =>
  queryOptions({
    queryKey: pinKeys.list(wsId, userId),
    queryFn: ({ signal }) => api.listPins({ signal }),
    enabled: !!wsId && !!userId,
  });
