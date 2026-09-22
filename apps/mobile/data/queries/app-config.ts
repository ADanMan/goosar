// Публичный конфиг развёртывания (GET /api/config), привязанный к мобильному
// api-клиенту и форме ключей кэша.
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const appConfigKeys = {
  all: ["app-config"] as const,
};

export const appConfigOptions = () =>
  queryOptions({
    queryKey: appConfigKeys.all,
    queryFn: ({ signal }) => api.getAppConfig({ signal }),
    staleTime: Infinity,
  });
