import type { EffectiveMcpConfigView } from '@goosar/core/api/effective-config';
import { isRecord, listManagedMcpServers } from '../agents/components/tabs/mcp-config-model';
import type { HelperMcpPresetEntry, HelperMcpPresetName } from '../onboarding/presets';
import {
  isPresetInstalled,
  SERVICE_DISPLAY_ORDER,
  serviceIdentityByPreset,
} from '../onboarding/presets';

export type AdminPartStatus =
  | 'provided'
  /** A server layer knows it but keeps it switched off. */
  | 'off'
  /** Nothing here can tell: no layer names it, or the view is unavailable. */
  | 'unknown';

export type PersonalPartStatus =
  | 'filled'
  /** At least one slot still holds its pristine catalog value. */
  | 'missing'
  /** This service has no personal credential at all. */
  | 'not_applicable'
  /** Not observable from here — see ABSENCE IS NOT EMPTINESS above. */
  | 'unknown';

export interface ServiceCredentialStatus {
  preset: HelperMcpPresetName;
  admin: AdminPartStatus;
  personal: PersonalPartStatus;
}

export interface CredentialStatusInput {
  helperMcpConfig: unknown;
  helperPresent: boolean;
  helperConfigRedacted: boolean;
  effectiveMcp: Record<string, EffectiveMcpConfigView> | undefined;
  installedPackageNames: readonly string[] | undefined;
  pristine: Record<string, HelperMcpPresetEntry>;
  catalogAvailable: boolean;
  corporateDeployment: boolean;
}

function stringField(record: Record<string, unknown>, key: string): string {
  const value = record[key];
  return typeof value === 'string' ? value : '';
}

export function isPersonalEnvSlotFilled(
  entryEnv: Record<string, unknown> | undefined,
  pristineEnv: Record<string, string> | undefined,
  key: string,
  unfilledPattern?: RegExp,
): boolean {
  const current = entryEnv ? stringField(entryEnv, key).trim() : '';
  if (current === '') return false;
  const seeded = (pristineEnv?.[key] ?? '').trim();
  if (current === seeded) return false;
  if (unfilledPattern && unfilledPattern.test(current)) return false;
  return true;
}

function layerViewFor(
  preset: HelperMcpPresetName,
  input: CredentialStatusInput,
): EffectiveMcpConfigView | undefined {
  const identity = serviceIdentityByPreset(preset);
  if (!identity || input.effectiveMcp === undefined) return undefined;
  for (const packageName of identity.packageNames) {
    const view = input.effectiveMcp[packageName];
    if (view) return view;
  }
  return undefined;
}

function layerEnvKeysFor(
  preset: HelperMcpPresetName,
  input: CredentialStatusInput,
): ReadonlySet<string> | undefined {
  const view = layerViewFor(preset, input);
  if (!view) return undefined;
  if (view.has_env !== true) {
    return view.has_env === false ? new Set<string>() : undefined;
  }
  if (view.env_keys === undefined) return undefined;
  return new Set(view.env_keys);
}

function ownEntryFor(
  preset: HelperMcpPresetName,
  input: CredentialStatusInput,
): { config: Record<string, unknown> } | null {
  return (
    listManagedMcpServers(input.helperMcpConfig).find((server) => server.name === preset) ?? null
  );
}

export function isServiceRelevant(
  preset: HelperMcpPresetName,
  input: CredentialStatusInput,
): boolean {
  if (layerViewFor(preset, input) !== undefined) return true;

  const entry = ownEntryFor(preset, input);
  if (entry && entry.config.enabled === true) return true;

  if (
    input.installedPackageNames !== undefined &&
    isPresetInstalled(preset, input.installedPackageNames)
  ) {
    return true;
  }

  if (input.corporateDeployment && input.catalogAvailable) {
    return serviceIdentityByPreset(preset)?.hasPersonalCredential === true;
  }
  return false;
}

function personalStatusFor(
  preset: HelperMcpPresetName,
  input: CredentialStatusInput,
): PersonalPartStatus {
  const identity = serviceIdentityByPreset(preset);
  if (!identity) return 'unknown';
  if (!identity.hasPersonalCredential) return 'not_applicable';
  if (!input.catalogAvailable) return 'unknown';
  if (!input.helperPresent) return 'unknown';
  if (input.helperConfigRedacted) return 'unknown';

  const layerKeys = layerEnvKeysFor(preset, input);
  const layerNamedSomething = layerViewFor(preset, input)?.has_env === true;
  if (layerNamedSomething && layerKeys === undefined) {
    return 'unknown';
  }
  const mySlots = identity.personalEnvKeys.filter(
    (key) => layerKeys === undefined || !layerKeys.has(key),
  );
  if (mySlots.length === 0) {
    return identity.personalEnvKeys.length > 0 ? 'unknown' : 'not_applicable';
  }

  const entry = ownEntryFor(preset, input);
  if (!entry) {
    return input.corporateDeployment ? 'missing' : 'unknown';
  }

  const pristineEntry = input.pristine[preset];
  const entryEnv = isRecord(entry.config.env) ? entry.config.env : undefined;
  for (const key of mySlots) {
    if (
      !isPersonalEnvSlotFilled(
        entryEnv,
        pristineEntry?.env,
        key,
        identity.unfilledEnvPatterns?.[key],
      )
    ) {
      return 'missing';
    }
  }
  return 'filled';
}

function adminStatusFor(
  preset: HelperMcpPresetName,
  input: CredentialStatusInput,
): AdminPartStatus {
  const view = layerViewFor(preset, input);
  if (!view) return 'unknown';
  return view.enabled === true ? 'provided' : 'off';
}

export function serviceCredentialStatuses(input: CredentialStatusInput): ServiceCredentialStatus[] {
  return SERVICE_DISPLAY_ORDER.filter((preset) => isServiceRelevant(preset, input)).map(
    (preset) => ({
      preset,
      admin: adminStatusFor(preset, input),
      personal: personalStatusFor(preset, input),
    }),
  );
}

export function isPersonalActionable(status: ServiceCredentialStatus): boolean {
  return status.personal === 'missing' && status.admin !== 'off';
}

export function missingPersonalCredentials(
  statuses: readonly ServiceCredentialStatus[],
): HelperMcpPresetName[] {
  return statuses.filter(isPersonalActionable).map((s) => s.preset);
}

export function missingCredentialsSignature(presets: readonly HelperMcpPresetName[]): string {
  return [...presets].sort().join(',');
}
