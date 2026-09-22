import { z } from 'zod';

export type EffectiveConfigOrigin =
  'policy' | 'workspace' | 'user_override' | 'machine' | (string & {});

export interface EffectiveLlmConfigView {
  base_url?: string;
  model?: string;
  has_api_key: boolean;
  origin: EffectiveConfigOrigin;
  locked: boolean;
}

export interface EffectiveMcpConfigView {
  enabled: boolean;
  has_env?: boolean;
  env_keys?: string[];
  origin: EffectiveConfigOrigin;
  locked?: boolean;
}

export interface EffectiveConfigView {
  schema_version: number;
  llm?: EffectiveLlmConfigView;
  mcp?: Record<string, EffectiveMcpConfigView>;
  revoked_packages?: string[];
}

export const EffectiveLlmConfigViewSchema = z.object({
  base_url: z.string().optional(),
  model: z.string().optional(),
  has_api_key: z.boolean().optional().default(false),
  origin: z.string().optional().default(''),
  locked: z.boolean().optional().default(false),
});

export const EffectiveMcpConfigViewSchema = z.object({
  enabled: z.boolean().optional().default(false),
  has_env: z.boolean().optional(),
  env_keys: z.array(z.string()).optional(),
  origin: z.string().optional().default(''),
  locked: z.boolean().optional(),
});

export const EffectiveConfigViewSchema = z.object({
  schema_version: z.number().optional().default(1),
  llm: EffectiveLlmConfigViewSchema.optional(),
  mcp: z.record(z.string(), EffectiveMcpConfigViewSchema).optional(),
  revoked_packages: z.array(z.string()).optional(),
});

export const EMPTY_EFFECTIVE_CONFIG_VIEW: EffectiveConfigView = {
  schema_version: 1,
};
