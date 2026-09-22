import { afterEach, describe, expect, it } from 'vitest';
import { configStore } from '@goosar/core/config';
import {
  listManagedMcpServers,
  validateMcpServerConfig,
} from '../../agents/components/tabs/mcp-config-model';
import {
  ATLASSIAN_LEGACY_BASIC_AUTH_FIELDS,
  buildHelperMcpConfig,
  HELPER_MCP_PRESET_NAMES,
  mergeMcpPresetOverlay,
  migrateAtlassianLegacyAuthFields,
} from './mcp-presets';

describe('buildHelperMcpConfig', () => {
  it('produces a document the MCP tab model parses into exactly the five presets', () => {
    const servers = listManagedMcpServers(buildHelperMcpConfig());
    expect(servers.map((s) => s.name).sort()).toEqual([...HELPER_MCP_PRESET_NAMES].sort());
    for (const server of servers) {
      expect(server.container).toBe('mcpServers');
      expect(server.transport).toBe('stdio');
      expect(server.enabled).toBe(false);
    }
  });

  it('every preset passes the MCP tab validation with no blocking errors', () => {
    const servers = listManagedMcpServers(buildHelperMcpConfig());
    expect(servers).toHaveLength(HELPER_MCP_PRESET_NAMES.length);
    for (const server of servers) {
      const { errors, warnings } = validateMcpServerConfig(server.config);
      expect(errors, `preset ${server.name}`).toEqual([]);
      expect(
        warnings.filter((w) => w !== 'command_unverified'),
        `preset ${server.name}`,
      ).toEqual([]);
    }
  });

  it('keeps credential slots empty for the user to fill in', () => {
    const { mcpServers } = buildHelperMcpConfig();
    expect(mcpServers.atlassian.env?.JIRA_PERSONAL_TOKEN).toBe('');
    expect(mcpServers.atlassian.env?.CONFLUENCE_PERSONAL_TOKEN).toBe('');
    expect(mcpServers.atlassian.env).not.toHaveProperty('JIRA_USERNAME');
    expect(mcpServers.atlassian.env).not.toHaveProperty('JIRA_API_TOKEN');
    expect(mcpServers.atlassian.env).not.toHaveProperty('CONFLUENCE_USERNAME');
    expect(mcpServers.atlassian.env).not.toHaveProperty('CONFLUENCE_API_TOKEN');
    expect(mcpServers.atlassian.env?.JIRA_SSL_VERIFY).toBe('true');
    expect(mcpServers.atlassian.env?.CONFLUENCE_SSL_VERIFY).toBe('true');
    expect(mcpServers.outlook.env?.EWS_EMAIL).toBe('');
    expect(mcpServers.bitrix24.env?.KB_API_TOKEN).toBe('');
    expect(mcpServers.bitrix24.env?.KB_SEARCH_DEPLOYMENT).toBe('');
    expect(mcpServers.bitrix24.env?.B24_WEBHOOK_URL).toBe('https://b24.corp.example/rest/');
    expect(mcpServers['mcp-gateway'].env?.API_ACCESS_TOKEN).toBe('');
  });

  it("never puts a credential-shaped value in any preset's args (issue #704)", () => {
    const CREDENTIAL_ARG_PATTERN = /(_TOKEN|_KEY|_SECRET|_PASSWORD|^Authorization$|^Bearer\b)/i;
    const { mcpServers } = buildHelperMcpConfig();
    for (const name of HELPER_MCP_PRESET_NAMES) {
      const args = mcpServers[name].args ?? [];
      for (const arg of args) {
        expect(arg, `preset ${name} arg ${JSON.stringify(arg)}`).not.toMatch(
          CREDENTIAL_ARG_PATTERN,
        );
      }
    }
  });

  it('uses bare command names, never per-user absolute paths', () => {
    const { mcpServers } = buildHelperMcpConfig();
    for (const name of HELPER_MCP_PRESET_NAMES) {
      const command = mcpServers[name].command;
      expect(command, name).not.toMatch(/[\\/]/);
      expect(command, name).not.toMatch(/\s/);
    }
  });

  it('never embeds CA-bundle env keys (network trust flows from the runtime host)', () => {
    const { mcpServers } = buildHelperMcpConfig();
    for (const name of HELPER_MCP_PRESET_NAMES) {
      const env = mcpServers[name].env ?? {};
      expect(Object.keys(env), name).not.toContain('REQUESTS_CA_BUNDLE');
      expect(Object.keys(env), name).not.toContain('SSL_CERT_FILE');
      expect(Object.keys(env), name).not.toContain('NODE_EXTRA_CA_CERTS');
    }
  });

  it('never commits an internal bitrix24/KB host in the default preset (issue #167)', () => {
    const doc = buildHelperMcpConfig(null);
    const hosts = JSON.stringify(doc).match(/https?:\/\/[A-Za-z0-9._-]+/g) ?? [];
    expect(hosts.length).toBeGreaterThan(0);
    for (const host of hosts) {
      expect(host).toMatch(
        /^https?:\/\/(?:127\.0\.0\.1|localhost|[A-Za-z0-9.-]+\.(?:example|test|invalid))$/,
      );
    }
    const env = doc.mcpServers.bitrix24.env ?? {};
    expect(env.B24_WEBHOOK_URL).toBe('https://b24.corp.example/rest/');
    expect(env.KB_BASE_URL).toBe('https://kb.corp.example');
  });

  it('does not disable TLS verification by default for bitrix24/KB (issue #167)', () => {
    const { mcpServers } = buildHelperMcpConfig(null);
    const env = mcpServers.bitrix24.env ?? {};
    expect(env.B24_VERIFY_SSL).toBeUndefined();
    expect(env.KB_VERIFY_SSL).toBeUndefined();
  });

  it('names only tools the packaged mcp-atlassian actually exposes', () => {
    const PACKAGED_TOOLS = new Set([
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
    ]);
    const { mcpServers } = buildHelperMcpConfig();

    for (const tool of mcpServers.atlassian.tools?.include ?? []) {
      expect(PACKAGED_TOOLS, `tool ${tool}`).toContain(tool);
    }
  });

  it('scopes atlassian and outlook to read-mostly tool include-lists', () => {
    const { mcpServers } = buildHelperMcpConfig();
    expect(mcpServers.atlassian.tools?.include).toContain('jira_search');
    expect(mcpServers.atlassian.tools?.include).toContain('confluence_get_page');
    expect(mcpServers.atlassian.tools?.include).toHaveLength(13);
    expect(mcpServers.outlook.tools?.include).toContain('search_messages');
    expect(mcpServers.outlook.tools?.include).toHaveLength(14);
    expect(mcpServers.bitrix24.tools?.include).toHaveLength(10);
  });

  it('returns a fresh document per call so callers cannot cross-mutate', () => {
    const first = buildHelperMcpConfig();
    const second = buildHelperMcpConfig();
    expect(first).not.toBe(second);
    expect(first.mcpServers.atlassian).not.toBe(second.mcpServers.atlassian);
    expect(first).toEqual(second);
  });
});

describe('provisioning overlay merge (issue #161)', () => {
  afterEach(() => {
    configStore.getState().setMcpPresetOverlay(null);
  });

  const overlay = {
    mcpServers: {
      atlassian: {
        env: {
          JIRA_URL: 'https://jira.corp.example',
          CONFLUENCE_URL: 'https://wiki.corp.example',
        },
      },
      'mcp-gateway': {
        args: ['--transport', 'streamablehttp', 'https://gateway.corp.example/mcp-proxy'],
      },
      'not-a-preset': { env: { X: 'y' } },
    },
  };

  it('deep-merges overlay addresses over the committed defaults', () => {
    const { mcpServers } = buildHelperMcpConfig(overlay);
    expect(mcpServers.atlassian.env?.JIRA_URL).toBe('https://jira.corp.example');
    expect(mcpServers.atlassian.env?.CONFLUENCE_URL).toBe('https://wiki.corp.example');
    expect(mcpServers['mcp-gateway'].args).toContain('https://gateway.corp.example/mcp-proxy');
    expect(mcpServers['mcp-gateway'].args).not.toContain(
      'https://mcp-gateway.corp.example/mcp-proxy',
    );
  });

  it('merges env per key — committed keys the overlay omits survive', () => {
    const { mcpServers } = buildHelperMcpConfig(overlay);
    expect(mcpServers.atlassian.env?.JIRA_PERSONAL_TOKEN).toBe('');
    expect(mcpServers.atlassian.env?.JIRA_CUSTOM_HEADERS).toContain('CorporateMCP/1.0');
  });

  it('ignores overlay entries for unknown server names', () => {
    const { mcpServers } = buildHelperMcpConfig(overlay);
    expect(Object.keys(mcpServers).sort()).toEqual([...HELPER_MCP_PRESET_NAMES].sort());
  });

  it('leaves presets the overlay does not mention at their committed default', () => {
    const { mcpServers } = buildHelperMcpConfig(overlay);
    expect(mcpServers.outlook.env?.EWS_SERVER_URL).toBe(
      'https://mail.corp.example/EWS/Exchange.asmx',
    );
  });

  it('keeps the committed defaults when there is no overlay', () => {
    const withNull = buildHelperMcpConfig(null);
    expect(withNull.mcpServers.atlassian.env?.JIRA_URL).toBe('https://jira.corp.example');
    expect(withNull).toEqual(buildHelperMcpConfig(null));
  });

  it('does not clobber a value the overlay does not provide', () => {
    const base = buildHelperMcpConfig(null);
    base.mcpServers.atlassian.env!.JIRA_PERSONAL_TOKEN = 'user-secret';
    const merged = mergeMcpPresetOverlay(base, {
      mcpServers: {
        atlassian: { env: { JIRA_URL: 'https://jira.corp.example' } },
      },
    });
    expect(merged.mcpServers.atlassian.env?.JIRA_PERSONAL_TOKEN).toBe('user-secret');
    expect(merged.mcpServers.atlassian.env?.JIRA_URL).toBe('https://jira.corp.example');
  });

  it('does not mutate the base document', () => {
    const base = buildHelperMcpConfig(null);
    const before = JSON.stringify(base);
    mergeMcpPresetOverlay(base, overlay);
    expect(JSON.stringify(base)).toBe(before);
  });

  it('reads the overlay from configStore by default', () => {
    configStore.getState().setMcpPresetOverlay(overlay);
    const { mcpServers } = buildHelperMcpConfig();
    expect(mcpServers.atlassian.env?.JIRA_URL).toBe('https://jira.corp.example');
  });

  it('degrades to the committed defaults on a malformed overlay', () => {
    const { mcpServers } = buildHelperMcpConfig({ mcpServers: 'nope' });
    expect(mcpServers.atlassian.env?.JIRA_URL).toBe('https://jira.corp.example');
  });
});

describe('migrateAtlassianLegacyAuthFields (T-10, #638)', () => {
  it('names exactly the four Cloud basic-auth fields removed by T-09', () => {
    expect([...ATLASSIAN_LEGACY_BASIC_AUTH_FIELDS].sort()).toEqual(
      ['CONFLUENCE_API_TOKEN', 'CONFLUENCE_USERNAME', 'JIRA_API_TOKEN', 'JIRA_USERNAME'].sort(),
    );
  });

  it('drops empty legacy fields', () => {
    const env = {
      JIRA_USERNAME: '',
      JIRA_API_TOKEN: '',
      CONFLUENCE_USERNAME: '',
      CONFLUENCE_API_TOKEN: '',
      JIRA_PERSONAL_TOKEN: 'pat',
    };
    const migrated = migrateAtlassianLegacyAuthFields(env);
    expect(migrated).not.toHaveProperty('JIRA_USERNAME');
    expect(migrated).not.toHaveProperty('JIRA_API_TOKEN');
    expect(migrated).not.toHaveProperty('CONFLUENCE_USERNAME');
    expect(migrated).not.toHaveProperty('CONFLUENCE_API_TOKEN');
    expect(migrated?.JIRA_PERSONAL_TOKEN).toBe('pat');
  });

  it('preserves non-empty Cloud fields (existing Cloud users keep working)', () => {
    const env = {
      JIRA_USERNAME: 'user@example.com',
      JIRA_API_TOKEN: 'cloud-token',
      CONFLUENCE_USERNAME: '',
      CONFLUENCE_API_TOKEN: '',
    };
    const migrated = migrateAtlassianLegacyAuthFields(env);
    expect(migrated?.JIRA_USERNAME).toBe('user@example.com');
    expect(migrated?.JIRA_API_TOKEN).toBe('cloud-token');
    expect(migrated).not.toHaveProperty('CONFLUENCE_USERNAME');
    expect(migrated).not.toHaveProperty('CONFLUENCE_API_TOKEN');
  });

  it('is a no-op when no legacy fields are present', () => {
    const env = { JIRA_PERSONAL_TOKEN: 'pat' };
    expect(migrateAtlassianLegacyAuthFields(env)).toEqual(env);
  });

  it('passes through undefined', () => {
    expect(migrateAtlassianLegacyAuthFields(undefined)).toBeUndefined();
  });
});
