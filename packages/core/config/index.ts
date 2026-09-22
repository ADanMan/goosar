import { createStore } from 'zustand/vanilla';
import { useStore } from 'zustand';
import {
  deploymentHostsFromMcpOverlay,
  EMPTY_DEPLOYMENT_HOSTS,
  mergeDeploymentHosts,
  parseDeploymentHosts,
  type DeploymentHosts,
} from './deployment-hosts';

export type ExternalImagesMode = 'allow' | 'block' | 'allowlist';

export function normalizeExternalImagesMode(raw?: string): ExternalImagesMode {
  const value = (raw ?? '').trim().toLowerCase();
  if (value === '' || value === 'allow') return 'allow';
  if (value === 'allowlist') return 'allowlist';
  return 'block';
}

interface ConfigState {
  cdnDomain: string;
  cdnSigned: boolean;
  allowSignup: boolean;
  daemonServerUrl: string;
  daemonAppUrl: string;
  workspaceCreationDisabled: boolean;
  vcsIntegrationAvailable: boolean;
  featureFlags: Record<string, boolean>;
  serverVersion: string;
  deliveryProfile: string;
  emailTransport: string;
  allowedProviders: string[];
  externalImages: ExternalImagesMode;
  imageHosts: string[];
  skillSources: string[] | null;
  mcpPresetOverlay: Record<string, unknown> | null;
  deploymentHosts: DeploymentHosts;
  setCdnConfig: (config: { cdnDomain: string; cdnSigned?: boolean }) => void;
  setImagePolicy: (config: { externalImages?: string; imageHosts?: string[] }) => void;
  setAuthConfig: (config: {
    allowSignup: boolean;
    workspaceCreationDisabled?: boolean;
    vcsIntegrationAvailable?: boolean;
  }) => void;
  setDaemonConfig: (config: { daemonServerUrl?: string; daemonAppUrl?: string }) => void;
  setFeatureFlags: (flags?: Record<string, boolean>) => void;
  setServerVersion: (version?: string) => void;
  setDeliveryProfile: (profile?: string) => void;
  setEmailTransport: (transport?: string) => void;
  setSkillSources: (sources?: string[]) => void;
  setAllowedProviders: (providers?: string[]) => void;
  setMcpPresetOverlay: (overlay?: Record<string, unknown> | null) => void;
  setDeploymentHosts: (raw?: unknown) => void;
}

export const configStore = createStore<ConfigState>((set) => ({
  cdnDomain: '',
  cdnSigned: false,
  allowSignup: true,
  daemonServerUrl: '',
  daemonAppUrl: '',
  workspaceCreationDisabled: false,
  vcsIntegrationAvailable: false,
  featureFlags: {},
  serverVersion: '',
  deliveryProfile: '',
  emailTransport: '',
  allowedProviders: [],
  externalImages: 'allow',
  imageHosts: [],
  skillSources: null,
  mcpPresetOverlay: null,
  deploymentHosts: EMPTY_DEPLOYMENT_HOSTS,
  setCdnConfig: ({ cdnDomain, cdnSigned = false }) => set({ cdnDomain, cdnSigned }),
  setImagePolicy: ({ externalImages, imageHosts = [] }) =>
    set({
      externalImages: normalizeExternalImagesMode(externalImages),
      imageHosts: [...imageHosts],
    }),
  setAuthConfig: ({
    allowSignup,
    workspaceCreationDisabled = false,
    vcsIntegrationAvailable = false,
  }) => set({ allowSignup, workspaceCreationDisabled, vcsIntegrationAvailable }),
  setDaemonConfig: ({ daemonServerUrl = '', daemonAppUrl = '' }) =>
    set({ daemonServerUrl, daemonAppUrl }),
  setFeatureFlags: (flags = {}) => set({ featureFlags: { ...flags } }),
  setServerVersion: (version = '') => set({ serverVersion: version }),
  setDeliveryProfile: (profile = '') => set({ deliveryProfile: profile }),
  setEmailTransport: (transport = '') => set({ emailTransport: transport }),
  setSkillSources: (sources) => set({ skillSources: sources ? [...sources] : null }),
  setAllowedProviders: (providers = []) => set({ allowedProviders: [...providers] }),
  setMcpPresetOverlay: (overlay) =>
    set((state) => {
      const value =
        overlay && typeof overlay === 'object' && !Array.isArray(overlay) ? overlay : null;
      return {
        mcpPresetOverlay: value,
        deploymentHosts: mergeDeploymentHosts(
          deploymentHostsFromMcpOverlay(value),
          state.deploymentHosts,
        ),
      };
    }),
  setDeploymentHosts: (raw) =>
    set((state) => ({
      deploymentHosts: mergeDeploymentHosts(state.deploymentHosts, parseDeploymentHosts(raw)),
    })),
}));

export function isProviderAllowedByPolicy(
  allowedProviders: readonly string[] | undefined,
  provider: string,
): boolean {
  if (!allowedProviders || allowedProviders.length === 0) return true;
  const slug = provider.trim().toLowerCase();
  return allowedProviders.some((entry) => entry.trim().toLowerCase() === slug);
}

export function isPerimeterDeliveryProfile(input: unknown): boolean {
  if (typeof input === 'string') return input === 'perimeter';
  if (typeof input !== 'object' || input === null) return false;
  const record = input as Record<string, unknown>;
  const profile = record['deliveryProfile'] ?? record['delivery_profile'];
  return profile === 'perimeter';
}

export type SkillSource = 'clawhub' | 'github' | 'skillssh';

export function isSkillSourceEnabled(
  sources: readonly string[] | null | undefined,
  source: SkillSource,
): boolean {
  if (sources == null) return true;
  return sources.includes(source);
}

export function anySkillSourceEnabled(sources: readonly string[] | null | undefined): boolean {
  return (
    isSkillSourceEnabled(sources, 'clawhub') ||
    isSkillSourceEnabled(sources, 'github') ||
    isSkillSourceEnabled(sources, 'skillssh')
  );
}

export function useConfigStore(): ConfigState;
export function useConfigStore<T>(selector: (state: ConfigState) => T): T;
export function useConfigStore<T>(selector?: (state: ConfigState) => T) {
  return useStore(configStore, selector as (state: ConfigState) => T);
}

export function featureFlagEnabled(
  flags: Readonly<Record<string, boolean>> | undefined,
  key: string,
  defaultValue = false,
): boolean {
  return flags?.[key] ?? defaultValue;
}

export function useFeatureEnabled(key: string, defaultValue = false): boolean {
  return useConfigStore((state) => featureFlagEnabled(state.featureFlags, key, defaultValue));
}

export {
  deploymentHostLabel,
  deploymentHostsFromMcpOverlay,
  EMPTY_DEPLOYMENT_HOSTS,
  mergeDeploymentHosts,
  parseDeploymentHosts,
  type DeploymentHosts,
} from './deployment-hosts';
