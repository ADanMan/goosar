import { z } from 'zod';

export interface McpConfigEntry {
  enabled?: boolean;
  env?: Record<string, boolean>;
}

const McpEnvHasValueSchema = z
  .union([z.boolean(), z.string()])
  .transform((v): boolean => v === true || (typeof v === 'string' && v.length > 0));

export const McpConfigEntrySchema = z.object({
  enabled: z.boolean().optional(),
  env: z.record(z.string(), McpEnvHasValueSchema).optional(),
});

const McpConfigDocSchema = z.record(z.string(), McpConfigEntrySchema);

export interface WorkspaceConfigView {
  llm_base_url?: string;
  llm_model?: string;
  has_llm_api_key: boolean;
  mcp_defaults?: Record<string, McpConfigEntry>;
  updated_at?: string;
  updated_by?: string;
}

export const WorkspaceConfigViewSchema = z.object({
  llm_base_url: z.string().optional(),
  llm_model: z.string().optional(),
  has_llm_api_key: z.boolean(),
  mcp_defaults: McpConfigDocSchema.optional(),
  updated_at: z.string().optional(),
  updated_by: z.string().optional(),
});

export const EMPTY_WORKSPACE_CONFIG_VIEW: WorkspaceConfigView = {
  has_llm_api_key: false,
};

export interface UserConfigOverrideView {
  user_id: string;
  llm_base_url?: string;
  llm_model?: string;
  has_llm_api_key: boolean;
  mcp_overrides?: Record<string, McpConfigEntry>;
  updated_at?: string;
  updated_by?: string;
}

export const UserConfigOverrideViewSchema = z.object({
  user_id: z.string(),
  llm_base_url: z.string().optional(),
  llm_model: z.string().optional(),
  has_llm_api_key: z.boolean(),
  mcp_overrides: McpConfigDocSchema.optional(),
  updated_at: z.string().optional(),
  updated_by: z.string().optional(),
});

export const EMPTY_USER_CONFIG_OVERRIDE_VIEW: UserConfigOverrideView = {
  user_id: '',
  has_llm_api_key: false,
};

export interface McpConfigEntryPatch {
  enabled?: boolean;
  env?: Record<string, string | null>;
}

export interface ConfigLayerPatch {
  llm_base_url?: string;
  llm_model?: string;
  llm_api_key?: string;
}

export interface WorkspaceConfigPatch extends ConfigLayerPatch {
  mcp_defaults?: Record<string, McpConfigEntryPatch | null> | null;
}

export interface UserConfigOverridePatch extends ConfigLayerPatch {
  mcp_overrides?: Record<string, McpConfigEntryPatch | null> | null;
}

export interface ProvisioningPin {
  package_name: string;
  package_type: string;
  version: string;
  enabled: boolean;
  updated_at?: string;
  updated_by?: string;
}

export interface ProvisioningPinInput {
  package_name: string;
  package_type: string;
  version: string;
  enabled: boolean;
}

export interface ProvisioningPinsView {
  pins: ProvisioningPin[];
}

const ProvisioningPinSchema = z.object({
  package_name: z.string(),
  package_type: z.string(),
  version: z.string(),
  enabled: z.boolean().optional().default(false),
  updated_at: z.string().optional(),
  updated_by: z.string().optional(),
});

export const ProvisioningPinsViewSchema = z.object({
  pins: z.array(ProvisioningPinSchema).optional().default([]),
});

export const EMPTY_PROVISIONING_PINS_VIEW: ProvisioningPinsView = { pins: [] };

export interface ProvisioningCatalogPackage {
  name: string;
  version: string;
  type: string;
  platform?: string;
  requires?: string[];
}

export interface ProvisioningCatalogView {
  schemaVersion?: number;
  packages: ProvisioningCatalogPackage[];
}

const ProvisioningCatalogPackageSchema = z.object({
  name: z.string(),
  version: z.string(),
  type: z.string(),
  platform: z.string().optional(),
  requires: z.array(z.string()).optional(),
});

export const ProvisioningCatalogViewSchema = z.object({
  schemaVersion: z.number().optional(),
  packages: z.array(ProvisioningCatalogPackageSchema).optional().default([]),
});

export const EMPTY_PROVISIONING_CATALOG_VIEW: ProvisioningCatalogView = {
  packages: [],
};
