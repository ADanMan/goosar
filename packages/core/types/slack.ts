/** Установка Slack-бота, привязанная к одному агенту Goosar.
 *
 * Форма повторяет `SlackInstallationResponse` в
 * `server/internal/handler/slack.go`. Новые поля, которые бэкенд добавит в
 * будущем, ДОЛЖНЫ быть опциональными по умолчанию, чтобы старые сборки
 * десктопа продолжали парсить ответ. */
export interface SlackInstallation {
  id: string;
  workspace_id: string;
  agent_id: string;
  team_id: string;
  bot_user_id: string;
  installer_user_id: string;
  status: 'active' | 'revoked' | string;
  installed_at: string;
  created_at: string;
  updated_at: string;
}

export interface ListSlackInstallationsResponse {
  installations: SlackInstallation[];
  configured: boolean;
  install_supported?: boolean;
}

export interface RegisterSlackBYORequest {
  bot_token: string;
  app_token: string;
}

export interface RedeemSlackBindingTokenResponse {
  workspace_id: string;
  installation_id: string;
  slack_user_id: string;
}
