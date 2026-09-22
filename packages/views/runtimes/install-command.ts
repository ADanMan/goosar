/**
 * Построители команд установки/настройки CLI, общие для диалога подключения
 * рантайма и инструкций онбординга CLI.
 */

export const CLOUD_SERVER_URL = 'https://goosar.ru';
export const CLOUD_APP_URL = 'https://goosar.ru';

function normalizeCommandURL(url: string | undefined) {
  return url?.trim().replace(/\/+$/, '') ?? '';
}

export function installCommand(serverUrl: string | undefined, appUrl: string | undefined) {
  const normalizedServerUrl = normalizeCommandURL(serverUrl) || CLOUD_SERVER_URL;
  const normalizedAppUrl = normalizeCommandURL(appUrl) || CLOUD_APP_URL;

  return `curl -fsSL ${normalizedAppUrl}/install.sh | bash -s -- --app-url ${normalizedAppUrl} --server-url ${normalizedServerUrl}`;
}

export function daemonCommands(serverUrl: string | undefined, appUrl: string | undefined) {
  const normalizedServerUrl = normalizeCommandURL(serverUrl);
  const normalizedAppUrl = normalizeCommandURL(appUrl);
  if (normalizedServerUrl && normalizedAppUrl) {
    return {
      setupCmd: `goosar setup self-host --server-url ${normalizedServerUrl} --app-url ${normalizedAppUrl}`,
      tokenCmd: `goosar config set server_url ${normalizedServerUrl}
goosar config set app_url ${normalizedAppUrl}
goosar login --token <YOUR_TOKEN>
goosar daemon start`,
    };
  }

  return {
    setupCmd: 'goosar setup',
    tokenCmd: `goosar config set server_url ${CLOUD_SERVER_URL}
goosar config set app_url ${CLOUD_APP_URL}
goosar login --token <YOUR_TOKEN>
goosar daemon start`,
  };
}
