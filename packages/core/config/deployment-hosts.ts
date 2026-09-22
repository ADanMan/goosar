/**
 * Хосты деплоя по умолчанию — единственный набор полей, называющий собственные
 * адреса заказчика, чтобы адреса конкретного деплоя никогда не попадали в репозиторий.
 *
 * Значения приходят из трёх источников, каждый заполняет только то, что не
 * заполнил предыдущий (см. `mergeDeploymentHosts`):
 *
 *   1. оверлей MCP-провижининга, который уже печёт деплой;
 *   2. блок `deployment` конфигурации, зашитой при сборке DMG;
 *   3. `GOOSAR_DEPLOYMENT_*` на сервере, отдаваемый через `/api/config` —
 *      так их узнаёт веб-приложение, у которого нет DMG.
 *
 * Всё здесь чистое и защищённое: источники — файлы оператора и сетевой JSON,
 * поэтому некорректное поле деградирует до "отсутствует" и никогда не бросает исключение.
 */

export interface DeploymentHosts {
  jiraUrl: string;
  confluenceUrl: string;
  ewsUrl: string;
  mailDomain: string;
  llmApiBase: string;
  llmModel: string;
}

export const EMPTY_DEPLOYMENT_HOSTS: DeploymentHosts = Object.freeze({
  jiraUrl: '',
  confluenceUrl: '',
  ewsUrl: '',
  mailDomain: '',
  llmApiBase: '',
  llmModel: '',
});

const FIELDS = Object.keys(EMPTY_DEPLOYMENT_HOSTS) as (keyof DeploymentHosts)[];

function readString(source: Record<string, unknown>, key: string): string {
  const value = source[key];
  return typeof value === 'string' ? value.trim() : '';
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

export function parseDeploymentHosts(raw: unknown): DeploymentHosts {
  const record = asRecord(raw);
  if (!record) return { ...EMPTY_DEPLOYMENT_HOSTS };
  const hosts = { ...EMPTY_DEPLOYMENT_HOSTS };
  for (const field of FIELDS) hosts[field] = readString(record, field);
  return hosts;
}

export function mergeDeploymentHosts(
  base: DeploymentHosts,
  patch: DeploymentHosts,
): DeploymentHosts {
  const merged = { ...base };
  for (const field of FIELDS) {
    if (patch[field]) merged[field] = patch[field];
  }
  return merged;
}

export function deploymentHostsFromMcpOverlay(overlay: unknown): DeploymentHosts {
  const servers = asRecord(asRecord(overlay)?.mcpServers);
  if (!servers) return { ...EMPTY_DEPLOYMENT_HOSTS };
  const atlassianEnv = asRecord(asRecord(servers.atlassian)?.env);
  const outlookEnv = asRecord(asRecord(servers.outlook)?.env);
  return {
    ...EMPTY_DEPLOYMENT_HOSTS,
    jiraUrl: atlassianEnv ? readString(atlassianEnv, 'JIRA_URL') : '',
    confluenceUrl: atlassianEnv ? readString(atlassianEnv, 'CONFLUENCE_URL') : '',
    ewsUrl: outlookEnv ? readString(outlookEnv, 'EWS_SERVER_URL') : '',
  };
}

export function deploymentHostLabel(url: string): string {
  const value = url.trim();
  if (!value) return '';
  try {
    return new URL(value.includes('://') ? value : `https://${value}`).hostname;
  } catch {
    return '';
  }
}
