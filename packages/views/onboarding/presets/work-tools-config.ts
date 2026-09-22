import {
  isRecord,
  listManagedMcpServers,
  upsertManagedMcpServer,
} from '../../agents/components/tabs/mcp-config-model';
import {
  buildHelperMcpConfig,
  migrateAtlassianLegacyAuthFields,
  type HelperMcpPresetEntry,
  type HelperMcpPresetName,
} from './mcp-presets';

export const WORK_TOOLS_CARD_ORDER: readonly HelperMcpPresetName[] = [
  'atlassian',
  'outlook',
  'bitrix24',
  'fetch',
  'mcp-gateway',
] as const;

export interface WorkToolsCredentials {
  atlassian: {
    jiraToken: string;
    confluenceToken: string;
    sslVerify: boolean;
  };
  outlook: {
    email: string;
  };
  bitrix24: {
    webhook: string;
    kbToken: string;
    kbDeployment: string;
  };
  'mcp-gateway': {
    bearerToken: string;
  };
  fetch: {
    enabled: boolean;
  };
}

export function emptyWorkToolsCredentials(): WorkToolsCredentials {
  return {
    atlassian: { jiraToken: '', confluenceToken: '', sslVerify: true },
    outlook: { email: '' },
    bitrix24: { webhook: '', kbToken: '', kbDeployment: '' },
    'mcp-gateway': { bearerToken: '' },
    fetch: { enabled: false },
  };
}

const isControlChar = (codePoint: number): boolean => codePoint <= 0x1f || codePoint === 0x7f;

export function sanitizeWorkToolsValue(value: string): string {
  let out = '';
  for (const ch of value) {
    if (!isControlChar(ch.codePointAt(0) ?? 0)) out += ch;
  }
  return out.trim();
}

const filled = (value: string): boolean => sanitizeWorkToolsValue(value).length > 0;

export function isWebhookInputAcceptable(input: string): boolean {
  const value = sanitizeWorkToolsValue(input);
  if (value.length === 0) return true;
  return !/\s/.test(value) && !value.includes('?') && !value.includes('#');
}

export function isWorkToolsPresetComplete(
  name: HelperMcpPresetName,
  credentials: WorkToolsCredentials,
): boolean {
  switch (name) {
    case 'atlassian':
      return (
        filled(credentials.atlassian.jiraToken) && filled(credentials.atlassian.confluenceToken)
      );
    case 'outlook':
      return filled(credentials.outlook.email);
    case 'bitrix24':
      return (
        filled(credentials.bitrix24.webhook) &&
        isWebhookInputAcceptable(credentials.bitrix24.webhook) &&
        filled(credentials.bitrix24.kbToken)
      );
    case 'mcp-gateway':
      return filled(credentials['mcp-gateway'].bearerToken);
    case 'fetch':
      return credentials.fetch.enabled === true;
  }
}

export function completedWorkToolsPresets(
  credentials: WorkToolsCredentials,
): HelperMcpPresetName[] {
  return WORK_TOOLS_CARD_ORDER.filter((name) => isWorkToolsPresetComplete(name, credentials));
}

function completeWebhookUrl(base: string, input: string): string {
  const trimmed = sanitizeWorkToolsValue(input);
  const withSlash = (url: string): string => (url.endsWith('/') ? url : `${url}/`);
  if (/^https?:\/\//i.test(trimmed)) return withSlash(trimmed);
  return withSlash(`${base}${trimmed.replace(/^\/+/, '')}`);
}

export type WorkToolsMcpEntry = Omit<HelperMcpPresetEntry, 'enabled'> & {
  enabled: boolean;
};

function buildCompletedPresetEntry(
  name: HelperMcpPresetName,
  credentials: WorkToolsCredentials,
): WorkToolsMcpEntry {
  const base = buildHelperMcpConfig().mcpServers[name];
  const entry: WorkToolsMcpEntry = {
    ...base,
    ...(base.env ? { env: { ...base.env } } : {}),
    ...(base.args ? { args: [...base.args] } : {}),
    enabled: true,
  };

  switch (name) {
    case 'atlassian': {
      if (entry.env) {
        entry.env.JIRA_PERSONAL_TOKEN = sanitizeWorkToolsValue(credentials.atlassian.jiraToken);
        entry.env.CONFLUENCE_PERSONAL_TOKEN = sanitizeWorkToolsValue(
          credentials.atlassian.confluenceToken,
        );
        const sslVerify = credentials.atlassian.sslVerify ? 'true' : 'false';
        entry.env.JIRA_SSL_VERIFY = sslVerify;
        entry.env.CONFLUENCE_SSL_VERIFY = sslVerify;
      }
      break;
    }
    case 'outlook': {
      if (entry.env) {
        entry.env.EWS_EMAIL = sanitizeWorkToolsValue(credentials.outlook.email);
      }
      break;
    }
    case 'bitrix24': {
      if (entry.env) {
        entry.env.B24_WEBHOOK_URL = completeWebhookUrl(
          entry.env.B24_WEBHOOK_URL ?? '',
          credentials.bitrix24.webhook,
        );
        entry.env.KB_API_TOKEN = sanitizeWorkToolsValue(credentials.bitrix24.kbToken);
        const kbDeployment = sanitizeWorkToolsValue(credentials.bitrix24.kbDeployment);
        if (kbDeployment) entry.env.KB_SEARCH_DEPLOYMENT = kbDeployment;
      }
      break;
    }
    case 'mcp-gateway': {
      if (entry.env) {
        entry.env.API_ACCESS_TOKEN = sanitizeWorkToolsValue(credentials['mcp-gateway'].bearerToken);
      }
      break;
    }
    case 'fetch':
      break;
  }

  return entry;
}

export function buildWorkToolsMcpConfig(
  credentials: WorkToolsCredentials,
  existingConfig?: unknown,
): Record<string, unknown> {
  const base: Record<string, unknown> = isRecord(existingConfig)
    ? { ...existingConfig }
    : { ...buildHelperMcpConfig() };
  const existingServers = listManagedMcpServers(base);

  let document = base;
  const completed = completedWorkToolsPresets(credentials);
  for (const name of completed) {
    const previous = existingServers.find((s) => s.name === name) ?? null;
    document = upsertManagedMcpServer(
      document,
      previous,
      name,
      buildCompletedPresetEntry(name, credentials) as unknown as Record<string, unknown>,
    );
  }

  if (!completed.includes('atlassian')) {
    const atlassian = listManagedMcpServers(document).find(
      (s) => s.name === 'atlassian' && !s.masked,
    );
    if (atlassian) {
      const env = isRecord(atlassian.config.env)
        ? (atlassian.config.env as Record<string, string>)
        : undefined;
      const migratedEnv = migrateAtlassianLegacyAuthFields(env);
      const changed =
        env !== undefined &&
        migratedEnv !== undefined &&
        Object.keys(migratedEnv).length !== Object.keys(env).length;
      if (changed) {
        document = upsertManagedMcpServer(document, atlassian, 'atlassian', {
          ...atlassian.config,
          env: migratedEnv,
        });
      }
    }
  }

  return document;
}
