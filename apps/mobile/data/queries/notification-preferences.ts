// Запрос настроек уведомлений, привязанный к мобильному api-клиенту.
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const notificationPreferenceKeys = {
  all: (wsId: string | null) => ["notification-preferences", wsId] as const,
};

export const notificationPreferenceOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: notificationPreferenceKeys.all(wsId),
    queryFn: ({ signal }) => api.getNotificationPreferences({ signal }),
    enabled: !!wsId,
  });
