import { z } from 'zod';

import { McpCredentialFieldSchema, type McpCredentialField } from './workspace-mcp';

export interface DeploymentMcpServer {
  id: string;
  name: string;
  transport: string;
  credential_schema: McpCredentialField[];
  enabled_workspaces?: number;
  enabled?: boolean;
  created_at?: string;
  updated_at?: string;
}

export const DeploymentMcpServerSchema = z.object({
  id: z.string(),
  name: z.string(),
  transport: z.string().optional().default('unknown'),
  credential_schema: z.array(McpCredentialFieldSchema).optional().default([]),
  enabled_workspaces: z.number().optional(),
  enabled: z.boolean().optional(),
  created_at: z.string().optional(),
  updated_at: z.string().optional(),
});

export const DeploymentMcpServerListSchema = z.array(DeploymentMcpServerSchema);

export const EMPTY_DEPLOYMENT_MCP_SERVERS: DeploymentMcpServer[] = [];

export interface DeploymentMcpServerInput {
  name?: string;
  config?: Record<string, unknown>;
  credential_schema?: McpCredentialField[];
}
