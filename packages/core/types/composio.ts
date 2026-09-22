/** Toolkit Composio из GET /api/integrations/composio/toolkits.
 *
 * Форма повторяет `ComposioToolkitResponse` в
 * `server/internal/handler/integrations_composio.go`. Новые поля, которые
 * добавит бэкенд позже, ДОЛЖНЫ оставаться опциональными, чтобы старые сборки
 * десктопа продолжали их парсить. */
export interface ComposioToolkit {
  slug: string;
  name: string;
  logo?: string;
  category?: string;
  connectable: boolean;
}

export interface ComposioConnection {
  id: string;
  toolkit_slug: string;
  status: 'active' | 'expired' | 'revoked' | string;
  connected_at: string;
  last_used_at?: string | null;
}

export interface ComposioConnectInitResponse {
  redirect_url: string;
}
