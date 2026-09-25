import { describe, expect, it } from 'vitest';
import { buildHelperMcpConfig } from '../onboarding/presets';
import type { HelperMcpPresetName } from '../onboarding/presets';
import {
  isPersonalActionable,
  missingCredentialsSignature,
  missingPersonalCredentials,
  serviceCredentialStatuses,
  type CredentialStatusInput,
} from './credential-status';

const PRISTINE = buildHelperMcpConfig().mcpServers;

function seededConfig(): Record<string, unknown> {
  return JSON.parse(JSON.stringify(buildHelperMcpConfig()));
}

function withEnv(
  config: Record<string, unknown>,
  preset: HelperMcpPresetName,
  env: Record<string, string>,
): Record<string, unknown> {
  const next = JSON.parse(JSON.stringify(config));
  const servers = next.mcpServers as Record<string, { env?: Record<string, string> }>;
  servers[preset] = {
    ...servers[preset],
    env: { ...(servers[preset]?.env ?? {}), ...env },
  };
  return next;
}

function withEnabled(
  config: Record<string, unknown>,
  presets: readonly HelperMcpPresetName[],
): Record<string, unknown> {
  const next = JSON.parse(JSON.stringify(config));
  const servers = next.mcpServers as Record<string, { enabled?: boolean }>;
  for (const preset of presets) {
    servers[preset] = { ...servers[preset], enabled: true };
  }
  return next;
}

const EVERY_SERVICE = {
  'ews-mcp': { enabled: true, origin: 'workspace' },
  'b24-agent': { enabled: true, origin: 'workspace' },
} as const;

function baseInput(overrides: Partial<CredentialStatusInput> = {}): CredentialStatusInput {
  return {
    helperMcpConfig: withEnabled(seededConfig(), ['atlassian', 'fetch', 'mcp-gateway']),
    helperPresent: true,
    helperConfigRedacted: false,
    effectiveMcp: { ...EVERY_SERVICE },
    installedPackageNames: undefined,
    pristine: PRISTINE,
    catalogAvailable: true,
    corporateDeployment: false,
    ...overrides,
  };
}

function statusFor(input: CredentialStatusInput, preset: HelperMcpPresetName) {
  const found = serviceCredentialStatuses(input).find((s) => s.preset === preset);
  if (!found) throw new Error(`no status for ${preset}`);
  return found;
}

describe("which services are this member's at all", () => {
  it('follows the role: an HR workspace gets the mailbox and nothing else', () => {
    const presets = serviceCredentialStatuses(
      baseInput({
        helperMcpConfig: seededConfig(),
        effectiveMcp: { 'ews-mcp': { enabled: true, origin: 'workspace' } },
      }),
    ).map((s) => s.preset);
    expect(presets).toEqual(['outlook']);
  });

  it('follows a DIFFERENT role to a different list, with no client-side branch', () => {
    const presets = serviceCredentialStatuses(
      baseInput({
        helperMcpConfig: seededConfig(),
        effectiveMcp: {
          'b24-agent': { enabled: true, origin: 'workspace' },
          'ews-mcp': { enabled: true, origin: 'workspace' },
        },
      }),
    ).map((s) => s.preset);
    expect(presets).toEqual(['outlook', 'bitrix24']);
  });

  it('shows nothing for a role that pins no service, and for a plain cloud tenant', () => {
    const presets = serviceCredentialStatuses(
      baseInput({ helperMcpConfig: seededConfig(), effectiveMcp: {} }),
    ).map((s) => s.preset);
    expect(presets).toEqual([]);
  });

  it('counts a service the member switched on themselves', () => {
    const presets = serviceCredentialStatuses(
      baseInput({
        helperMcpConfig: withEnabled(seededConfig(), ['atlassian']),
        effectiveMcp: {},
      }),
    ).map((s) => s.preset);
    expect(presets).toEqual(['atlassian']);
  });

  it('counts a service the deployment installed on this machine', () => {
    const presets = serviceCredentialStatuses(
      baseInput({
        helperMcpConfig: seededConfig(),
        effectiveMcp: {},
        installedPackageNames: ['ews-mcp'],
      }),
    ).map((s) => s.preset);
    expect(presets).toEqual(['outlook']);
  });

  it('offers the whole corporate catalog to a granted member who skipped onboarding', () => {
    const input = baseInput({
      corporateDeployment: true,
      helperMcpConfig: { mcpServers: {} },
      effectiveMcp: {},
    });
    expect(serviceCredentialStatuses(input).map((s) => s.preset)).toEqual([
      'atlassian',
      'outlook',
      'bitrix24',
      'mcp-gateway',
    ]);
    expect(missingPersonalCredentials(serviceCredentialStatuses(input))).toEqual([
      'atlassian',
      'outlook',
      'bitrix24',
      'mcp-gateway',
    ]);
  });

  it('offers nothing to a member the corporate catalog is withheld from', () => {
    expect(
      serviceCredentialStatuses(
        baseInput({
          corporateDeployment: true,
          catalogAvailable: false,
          helperMcpConfig: { mcpServers: {} },
          effectiveMcp: {},
        }),
      ),
    ).toEqual([]);
  });

  it("keeps the corporate catalog off a cloud tenant's page", () => {
    expect(
      serviceCredentialStatuses(
        baseInput({
          corporateDeployment: false,
          helperMcpConfig: seededConfig(),
          effectiveMcp: {},
        }),
      ),
    ).toEqual([]);
  });

  it('keeps display order regardless of which source contributed each service', () => {
    const presets = serviceCredentialStatuses(
      baseInput({
        helperMcpConfig: withEnabled(seededConfig(), ['mcp-gateway']),
        effectiveMcp: { 'ews-mcp': { enabled: true, origin: 'workspace' } },
        installedPackageNames: ['atlassian'],
      }),
    ).map((s) => s.preset);
    expect(presets).toEqual(['atlassian', 'outlook', 'mcp-gateway']);
  });
});

describe("the administrator's half", () => {
  it('reads a service the administrator enabled as provided', () => {
    expect(statusFor(baseInput(), 'outlook').admin).toBe('provided');
  });

  it('distinguishes an administrator who switched a service off', () => {
    const status = statusFor(
      baseInput({
        effectiveMcp: { 'b24-agent': { enabled: false, origin: 'policy' } },
        helperMcpConfig: withEnabled(seededConfig(), ['bitrix24']),
      }),
      'bitrix24',
    );
    expect(status.admin).toBe('off');
  });

  it("says 'unknown', never 'somebody is preparing it', when no layer names the service", () => {
    expect(statusFor(baseInput(), 'atlassian').admin).toBe('unknown');
    expect(
      statusFor(
        baseInput({
          effectiveMcp: undefined,
          installedPackageNames: ['ews-mcp'],
        }),
        'outlook',
      ).admin,
    ).toBe('unknown');
  });
});

describe("the member's own half", () => {
  it("reads an untouched catalog entry as needing the member's key", () => {
    const input = baseInput();
    expect(statusFor(input, 'atlassian').personal).toBe('missing');
    expect(statusFor(input, 'outlook').personal).toBe('missing');
    expect(statusFor(input, 'bitrix24').personal).toBe('missing');
    expect(statusFor(input, 'mcp-gateway').personal).toBe('missing');
  });

  it('reads a filled entry as done', () => {
    let config = withEnv(baseInput().helperMcpConfig as Record<string, unknown>, 'atlassian', {
      JIRA_PERSONAL_TOKEN: 'pat-jira',
      CONFLUENCE_PERSONAL_TOKEN: 'pat-conf',
    });
    config = withEnv(config, 'outlook', { EWS_EMAIL: 'me@corp.example' });
    const input = baseInput({ helperMcpConfig: config });
    expect(statusFor(input, 'atlassian').personal).toBe('filled');
    expect(statusFor(input, 'outlook').personal).toBe('filled');
  });

  it('still asks when only part of a service is filled in', () => {
    const config = withEnv(baseInput().helperMcpConfig as Record<string, unknown>, 'atlassian', {
      JIRA_PERSONAL_TOKEN: 'pat-jira',
    });
    expect(statusFor(baseInput({ helperMcpConfig: config }), 'atlassian').personal).toBe('missing');
  });

  it('does not mistake the seeded webhook base for a filled-in webhook', () => {
    const seededBase = PRISTINE['bitrix24']?.env?.B24_WEBHOOK_URL ?? '';
    expect(seededBase).not.toBe('');

    const untouched = withEnv(seededConfig(), 'bitrix24', {
      KB_API_TOKEN: 'kb-token',
    });
    expect(statusFor(baseInput({ helperMcpConfig: untouched }), 'bitrix24').personal).toBe(
      'missing',
    );

    const completed = withEnv(untouched, 'bitrix24', {
      B24_WEBHOOK_URL: `${seededBase}123/abcdef/`,
    });
    expect(statusFor(baseInput({ helperMcpConfig: completed }), 'bitrix24').personal).toBe(
      'filled',
    );
  });

  it("does not mistake ANOTHER deployment's webhook base for a filled-in webhook", () => {
    const other = withEnv(seededConfig(), 'bitrix24', {
      B24_WEBHOOK_URL: 'https://b24.some-real-host.example/rest/',
      KB_API_TOKEN: 'kb-token',
    });
    expect(statusFor(baseInput({ helperMcpConfig: other }), 'bitrix24').personal).toBe('missing');
  });

  it("treats the gateway's untouched API_ACCESS_TOKEN as missing, filled once set (issue #704)", () => {
    const filled = withEnv(baseInput().helperMcpConfig as Record<string, unknown>, 'mcp-gateway', {
      API_ACCESS_TOKEN: 'real-token',
    });
    expect(statusFor(baseInput({ helperMcpConfig: filled }), 'mcp-gateway').personal).toBe(
      'filled',
    );
    expect(statusFor(baseInput(), 'mcp-gateway').personal).toBe('missing');
  });

  it('never asks for a key the service does not have', () => {
    expect(statusFor(baseInput(), 'fetch').personal).toBe('not_applicable');
  });
});

describe('refusing to guess', () => {
  it('says nothing when the member has no Helper yet', () => {
    const input = baseInput({ helperMcpConfig: null, helperPresent: false });
    expect(statusFor(input, 'outlook').personal).toBe('unknown');
    expect(missingPersonalCredentials(serviceCredentialStatuses(input))).toEqual([]);
  });

  it('says nothing when the configuration is redacted for this viewer', () => {
    const input = baseInput({
      helperMcpConfig: null,
      helperConfigRedacted: true,
    });
    expect(missingPersonalCredentials(serviceCredentialStatuses(input))).toEqual([]);
  });

  it('says nothing when the catalog was never seeded for this member', () => {
    const input = baseInput({ helperMcpConfig: { mcpServers: {} } });
    expect(missingPersonalCredentials(serviceCredentialStatuses(input))).toEqual([]);
  });

  it('says nothing when the member has no access to the corporate catalog', () => {
    const input = baseInput({ catalogAvailable: false });
    expect(missingPersonalCredentials(serviceCredentialStatuses(input))).toEqual([]);
  });

  it('says nothing when a configuration layer already supplies the values', () => {
    const input = baseInput({
      helperMcpConfig: seededConfig(),
      effectiveMcp: {
        'ews-mcp': { enabled: true, has_env: true, origin: 'workspace' },
      },
    });
    expect(statusFor(input, 'outlook').personal).toBe('unknown');
    expect(missingPersonalCredentials(serviceCredentialStatuses(input))).toEqual([]);
  });

  it("still asks for the member's own slot when the layer filled a DIFFERENT one", () => {
    const input = baseInput({
      helperMcpConfig: seededConfig(),
      effectiveMcp: {
        'ews-mcp': {
          enabled: true,
          has_env: true,
          env_keys: ['EWS_SERVER_URL', 'EWS_TZ'],
          origin: 'workspace',
        },
      },
    });
    expect(statusFor(input, 'outlook').personal).toBe('missing');
    expect(missingPersonalCredentials(serviceCredentialStatuses(input))).toEqual(['outlook']);
  });

  it('stops asking for a slot the layer names as its own', () => {
    const input = baseInput({
      helperMcpConfig: seededConfig(),
      effectiveMcp: {
        'ews-mcp': {
          enabled: true,
          has_env: true,
          env_keys: ['EWS_EMAIL', 'EWS_SERVER_URL'],
          origin: 'workspace',
        },
      },
    });
    expect(statusFor(input, 'outlook').personal).toBe('unknown');
    expect(missingPersonalCredentials(serviceCredentialStatuses(input))).toEqual([]);
  });

  it('keeps its mouth shut against a backend that cannot name its slots', () => {
    const input = baseInput({
      helperMcpConfig: seededConfig(),
      effectiveMcp: {
        'ews-mcp': { enabled: true, has_env: true, origin: 'workspace' },
      },
    });
    expect(statusFor(input, 'outlook').personal).toBe('unknown');
  });

  it('still asks when the layer enables a service but supplies nothing', () => {
    const input = baseInput({
      helperMcpConfig: seededConfig(),
      effectiveMcp: {
        'ews-mcp': { enabled: true, has_env: false, origin: 'workspace' },
      },
    });
    expect(statusFor(input, 'outlook').personal).toBe('missing');
  });
});

describe('the list the banner is built from', () => {
  it('lists exactly the services whose personal half is missing, in display order', () => {
    let config = withEnv(baseInput().helperMcpConfig as Record<string, unknown>, 'atlassian', {
      JIRA_PERSONAL_TOKEN: 'pat-jira',
      CONFLUENCE_PERSONAL_TOKEN: 'pat-conf',
    });
    config = withEnv(config, 'bitrix24', {
      B24_WEBHOOK_URL: 'https://b24.corp.example/rest/1/abc/',
      KB_API_TOKEN: 'kb',
    });
    const missing = missingPersonalCredentials(
      serviceCredentialStatuses(baseInput({ helperMcpConfig: config })),
    );
    expect(missing).toEqual(['outlook', 'mcp-gateway']);
  });

  it('empties out once every key is entered', () => {
    let config = withEnv(baseInput().helperMcpConfig as Record<string, unknown>, 'atlassian', {
      JIRA_PERSONAL_TOKEN: 'j',
      CONFLUENCE_PERSONAL_TOKEN: 'c',
    });
    config = withEnv(config, 'outlook', { EWS_EMAIL: 'me@corp.example' });
    config = withEnv(config, 'bitrix24', {
      B24_WEBHOOK_URL: 'https://b24.corp.example/rest/1/abc/',
      KB_API_TOKEN: 'kb',
    });
    config = withEnv(config, 'mcp-gateway', { API_ACCESS_TOKEN: 'real-token' });
    expect(
      missingPersonalCredentials(serviceCredentialStatuses(baseInput({ helperMcpConfig: config }))),
    ).toEqual([]);
  });

  it('stays empty for a member whose workspace has no corporate service', () => {
    expect(
      missingPersonalCredentials(
        serviceCredentialStatuses(baseInput({ helperMcpConfig: seededConfig(), effectiveMcp: {} })),
      ),
    ).toEqual([]);
  });

  it('does not chase a key for a service an administrator switched off', () => {
    const input = baseInput({
      helperMcpConfig: seededConfig(),
      effectiveMcp: {
        'b24-agent': { enabled: false, locked: true, origin: 'policy' },
      },
    });
    expect(statusFor(input, 'bitrix24').admin).toBe('off');
    expect(statusFor(input, 'bitrix24').personal).toBe('missing');
    expect(isPersonalActionable(statusFor(input, 'bitrix24'))).toBe(false);
    expect(missingPersonalCredentials(serviceCredentialStatuses(input))).toEqual([]);
  });

  it('resumes chasing it the moment the administrator switches the service on', () => {
    const input = baseInput({
      helperMcpConfig: seededConfig(),
      effectiveMcp: {
        'b24-agent': { enabled: true, origin: 'policy' },
      },
    });
    expect(isPersonalActionable(statusFor(input, 'bitrix24'))).toBe(true);
    expect(missingPersonalCredentials(serviceCredentialStatuses(input))).toEqual(['bitrix24']);
  });

  it('fingerprints the list order-independently so a reorder is not a new list', () => {
    expect(missingCredentialsSignature(['outlook', 'atlassian'])).toBe(
      missingCredentialsSignature(['atlassian', 'outlook']),
    );
    expect(missingCredentialsSignature(['atlassian'])).not.toBe(
      missingCredentialsSignature(['atlassian', 'outlook']),
    );
    expect(missingCredentialsSignature([])).toBe('');
  });
});
