import type { HelperMcpPresetName } from './mcp-presets';
import { WORK_TOOLS_CARD_ORDER } from './work-tools-config';

export interface ServiceIdentity {
  preset: HelperMcpPresetName;
  packageNames: readonly string[];
  personalEnvKeys: readonly string[];
  unfilledEnvPatterns?: Readonly<Record<string, RegExp>>;
  hasPersonalCredential: boolean;
}

export const SERVICE_IDENTITIES: readonly ServiceIdentity[] = [
  {
    preset: 'atlassian',
    packageNames: [],
    personalEnvKeys: ['JIRA_PERSONAL_TOKEN', 'CONFLUENCE_PERSONAL_TOKEN'],
    hasPersonalCredential: true,
  },
  {
    preset: 'outlook',
    packageNames: ['ews-mcp'],
    personalEnvKeys: ['EWS_EMAIL'],
    hasPersonalCredential: true,
  },
  {
    preset: 'bitrix24',
    packageNames: ['b24-agent'],
    personalEnvKeys: ['B24_WEBHOOK_URL', 'KB_API_TOKEN'],
    unfilledEnvPatterns: { B24_WEBHOOK_URL: /\/rest\/?$/ },
    hasPersonalCredential: true,
  },
  {
    preset: 'fetch',
    packageNames: [],
    personalEnvKeys: [],
    hasPersonalCredential: false,
  },
  {
    preset: 'mcp-gateway',
    packageNames: [],
    personalEnvKeys: ['API_ACCESS_TOKEN'],
    hasPersonalCredential: true,
  },
] as const;

export const PUBLIC_INDEX_PRESETS: readonly HelperMcpPresetName[] = SERVICE_IDENTITIES.filter(
  (s) => s.packageNames.length === 0,
).map((s) => s.preset);

export function serviceIdentityByPreset(preset: string): ServiceIdentity | null {
  return SERVICE_IDENTITIES.find((s) => s.preset === preset) ?? null;
}

export function serviceIdentityByPackageName(packageName: string): ServiceIdentity | null {
  return SERVICE_IDENTITIES.find((s) => s.packageNames.includes(packageName)) ?? null;
}

export const SERVICE_DISPLAY_ORDER: readonly HelperMcpPresetName[] = WORK_TOOLS_CARD_ORDER;

export function isPresetInstalled(
  preset: HelperMcpPresetName,
  installedNames: readonly string[],
): boolean {
  if (installedNames.includes(preset)) return true;
  const identity = serviceIdentityByPreset(preset);
  if (!identity) return false;
  return identity.packageNames.some((name) => installedNames.includes(name));
}
