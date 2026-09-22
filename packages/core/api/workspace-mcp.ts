import { z } from 'zod';

export interface McpCredentialField {
  key: string;
  label?: string;
  hint?: string;
  required: boolean;
}

export interface WorkspaceMcpServer {
  id: string;
  workspace_id: string;
  name: string;
  transport: string;
  source: string;
  enabled?: boolean;
  credential_schema: McpCredentialField[];
  provided_credentials: string[];
  missing_credentials: string[];
  created_at?: string;
  updated_at?: string;
}

export const McpCredentialFieldSchema = z.object({
  key: z.string(),
  label: z.string().optional(),
  hint: z.string().optional(),
  required: z.boolean().optional().default(false),
});

export const WorkspaceMcpServerSchema = z.object({
  id: z.string(),
  workspace_id: z.string().optional().default(''),
  name: z.string(),
  transport: z.string().optional().default('unknown'),
  source: z.string().optional().default('workspace'),
  enabled: z.boolean().optional(),
  credential_schema: z.array(McpCredentialFieldSchema).optional().default([]),
  provided_credentials: z.array(z.string()).optional().default([]),
  missing_credentials: z.array(z.string()).optional().default([]),
  created_at: z.string().optional(),
  updated_at: z.string().optional(),
});

export const WorkspaceMcpServerListSchema = z.array(WorkspaceMcpServerSchema);

export const EMPTY_WORKSPACE_MCP_SERVERS: WorkspaceMcpServer[] = [];

export interface WorkspaceMcpServerInput {
  name?: string;
  config?: Record<string, unknown>;
  credential_schema?: McpCredentialField[];
}

export interface McpCredentialValuesInput {
  values: Record<string, string>;
}
