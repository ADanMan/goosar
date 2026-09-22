// Корпоративные пресеты MCP, засеваемые в mcp_config агента-помощника
// при его создании после онбординга.

import { configStore } from '@goosar/core/config';

export const HELPER_MCP_PRESET_NAMES = [
  'atlassian',
  'fetch',
  'mcp-gateway',
  'outlook',
  'bitrix24',
] as const;

export type HelperMcpPresetName = (typeof HELPER_MCP_PRESET_NAMES)[number];

export interface HelperMcpPresetEntry {
  command: string;
  args?: string[];
  env?: Record<string, string>;
  enabled: false;
  timeout: number;
  init_timeout: number;
  tools?: { include: string[] };
}

const LOCAL_PROXY_URL = 'http://127.0.0.1:3128';

function atlassianPreset(): HelperMcpPresetEntry {
  return {
    command: 'mcp-atlassian',
    env: {
      JIRA_URL: 'https://jira.corp.example',
      JIRA_PERSONAL_TOKEN: '',
      JIRA_CUSTOM_HEADERS: 'User-Agent=CorporateMCP/1.0 (integration)',
      JIRA_SSL_VERIFY: 'true',
      CONFLUENCE_URL: 'https://wiki.corp.example',
      CONFLUENCE_PERSONAL_TOKEN: '',
      CONFLUENCE_CUSTOM_HEADERS: 'User-Agent=CorporateMCP/1.0 (integration)',
      CONFLUENCE_SSL_VERIFY: 'true',
    },
    enabled: false,
    init_timeout: 60,
    timeout: 60,
    tools: {
      include: [
        'confluence_search',
        'confluence_get_space_page_tree',
        'confluence_get_page',
        'confluence_get_page_children',
        'confluence_get_comments',
        'confluence_get_labels',
        'confluence_get_page_history',
        'confluence_get_attachments',
        'jira_search',
        'jira_get_issue',
        'jira_get_all_projects',
        'jira_get_project_issues',
        'jira_get_transitions',
      ],
    },
  };
}

function fetchPreset(): HelperMcpPresetEntry {
  return {
    command: 'mcp-server-fetch',
    args: ['--proxy-url', LOCAL_PROXY_URL, '--ignore-robots-txt'],
    enabled: false,
    init_timeout: 30,
    timeout: 60,
  };
}

function mcpGatewayPreset(): HelperMcpPresetEntry {
  return {
    command: 'mcp-proxy',
    env: {
      API_ACCESS_TOKEN: '',
    },
    args: ['--transport', 'streamablehttp', 'https://mcp-gateway.corp.example/mcp-proxy'],
    enabled: false,
    init_timeout: 90,
    timeout: 120,
  };
}

function outlookPreset(): HelperMcpPresetEntry {
  return {
    command: 'ewsmcp',
    env: {
      EWS_SERVER_URL: 'https://mail.corp.example/EWS/Exchange.asmx',
      EWS_EMAIL: '',
      EWS_AUTH_KERBEROS: 'true',
      EWS_TZ: 'Europe/Moscow',
      EWS_CAPABILITY_TIER: 'read',
      EWS_SEND_ENABLED: 'false',
      EWS_CACHE_ENABLED: 'true',
    },
    enabled: false,
    init_timeout: 90,
    timeout: 90,
    tools: {
      include: [
        'get_mailbox_overview',
        'list_folders',
        'search_messages',
        'get_message',
        'get_thread',
        'get_attachment',
        'waiting_on',
        'find_similar',
        'find_people',
        'get_contact',
        'list_events',
        'get_event',
        'check_availability',
        'get_server_status',
      ],
    },
  };
}

function bitrix24Preset(): HelperMcpPresetEntry {
  return {
    command: 'mcp-server-b24',
    env: {
      B24_WEBHOOK_URL: 'https://b24.corp.example/rest/',
      HTTPS_PROXY: LOCAL_PROXY_URL,
      HTTP_PROXY: LOCAL_PROXY_URL,
      NO_PROXY: 'localhost,127.0.0.1',
      KB_API_TOKEN: '',
      KB_BASE_URL: 'https://kb.corp.example',
      KB_SEARCH_DEPLOYMENT: '',
    },
    enabled: false,
    init_timeout: 60,
    timeout: 60,
    tools: {
      include: [
        'list_deals',
        'get_deal',
        'get_deal_status',
        'list_deal_comments',
        'list_deal_questions',
        'find_productolog_field',
        'list_knowledge_bases',
        'search_knowledge',
        'search_emails',
        'list_recent_emails',
      ],
    },
  };
}

const PRESET_BUILDERS: Record<HelperMcpPresetName, () => HelperMcpPresetEntry> = {
  atlassian: atlassianPreset,
  fetch: fetchPreset,
  'mcp-gateway': mcpGatewayPreset,
  outlook: outlookPreset,
  bitrix24: bitrix24Preset,
};

export interface McpPresetOverlayEntry {
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  timeout?: number;
  init_timeout?: number;
}

export interface McpPresetOverlay {
  mcpServers?: Partial<Record<HelperMcpPresetName, McpPresetOverlayEntry>>;
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function overlayEntryFor(
  overlay: unknown,
  name: HelperMcpPresetName,
): McpPresetOverlayEntry | null {
  const root = asRecord(overlay);
  const servers = root && asRecord(root.mcpServers);
  const entry = servers && asRecord(servers[name]);
  if (!entry) return null;

  const patch: McpPresetOverlayEntry = {};
  if (typeof entry.command === 'string' && entry.command.trim().length > 0) {
    patch.command = entry.command;
  }
  if (Array.isArray(entry.args) && entry.args.every((arg) => typeof arg === 'string')) {
    patch.args = [...(entry.args as string[])];
  }
  const env = asRecord(entry.env);
  if (env) {
    const clean: Record<string, string> = {};
    for (const [key, value] of Object.entries(env)) {
      if (typeof value === 'string') clean[key] = value;
    }
    if (Object.keys(clean).length > 0) patch.env = clean;
  }
  if (typeof entry.timeout === 'number' && Number.isFinite(entry.timeout)) {
    patch.timeout = entry.timeout;
  }
  if (typeof entry.init_timeout === 'number' && Number.isFinite(entry.init_timeout)) {
    patch.init_timeout = entry.init_timeout;
  }
  return patch;
}

export function mergeMcpPresetOverlay(
  base: { mcpServers: Record<HelperMcpPresetName, HelperMcpPresetEntry> },
  overlay: unknown,
): { mcpServers: Record<HelperMcpPresetName, HelperMcpPresetEntry> } {
  const mcpServers = {} as Record<HelperMcpPresetName, HelperMcpPresetEntry>;
  for (const name of HELPER_MCP_PRESET_NAMES) {
    const baseEntry = base.mcpServers[name];
    const patch = overlayEntryFor(overlay, name);
    if (!patch) {
      mcpServers[name] = baseEntry;
      continue;
    }
    mcpServers[name] = {
      ...baseEntry,
      ...(patch.command ? { command: patch.command } : {}),
      ...(patch.args ? { args: patch.args } : {}),
      ...(patch.timeout !== undefined ? { timeout: patch.timeout } : {}),
      ...(patch.init_timeout !== undefined ? { init_timeout: patch.init_timeout } : {}),
      ...(patch.env ? { env: { ...(baseEntry.env ?? {}), ...patch.env } } : {}),
    };
  }
  return { mcpServers };
}

export function buildHelperMcpConfig(overlay: unknown = configStore.getState().mcpPresetOverlay): {
  mcpServers: Record<HelperMcpPresetName, HelperMcpPresetEntry>;
} {
  const mcpServers = {} as Record<HelperMcpPresetName, HelperMcpPresetEntry>;
  for (const name of HELPER_MCP_PRESET_NAMES) {
    mcpServers[name] = PRESET_BUILDERS[name]();
  }
  const base = { mcpServers };
  return overlay ? mergeMcpPresetOverlay(base, overlay) : base;
}

export const ATLASSIAN_LEGACY_BASIC_AUTH_FIELDS = [
  'JIRA_USERNAME',
  'JIRA_API_TOKEN',
  'CONFLUENCE_USERNAME',
  'CONFLUENCE_API_TOKEN',
] as const;

export function migrateAtlassianLegacyAuthFields(
  env: Record<string, string> | undefined,
): Record<string, string> | undefined {
  if (!env) return env;
  const next: Record<string, string> = { ...env };
  for (const key of ATLASSIAN_LEGACY_BASIC_AUTH_FIELDS) {
    if (key in next && next[key] === '') delete next[key];
  }
  return next;
}
