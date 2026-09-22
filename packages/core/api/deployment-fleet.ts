import { z } from 'zod';

export interface DeploymentFleetAgent {
  id: string;
  name: string;
  system_key: string;
  status: string;
}

export interface DeploymentFleetRuntime {
  id: string;
  name: string;
  provider: string;
  visibility: string;
  status: string;
  online: boolean;
  last_seen_at: string | null;
  running_tasks: number;
  stuck_tasks: number;
  agents: DeploymentFleetAgent[];
}

export interface DeploymentFleetMachine {
  daemon_id: string;
  workspace_id: string;
  workspace_name: string;
  workspace_slug: string;
  owner_email: string;
  owner_name: string;
  device_info: string;
  online: boolean;
  last_heartbeat_at: string | null;
  client_version: string;
  version_outdated: boolean;
  running_tasks: number;
  stuck_tasks: number;
  runtimes: DeploymentFleetRuntime[];
}

export interface DeploymentFleetSummary {
  machines_total: number;
  machines_online: number;
  machines_offline: number;
  outdated_versions: number;
  running_tasks: number;
  stuck_tasks: number;
}

export interface DeploymentFleet {
  machines: DeploymentFleetMachine[];
  summary: DeploymentFleetSummary;
  total: number;
  truncated: boolean;
  min_client_version: string;
}

const nullableIso = z
  .string()
  .nullish()
  .transform((v) => v ?? null);

export const DeploymentFleetAgentSchema = z.object({
  id: z.string().optional().default(''),
  name: z.string().optional().default(''),
  system_key: z.string().optional().default(''),
  status: z.string().optional().default(''),
});

export const DeploymentFleetRuntimeSchema = z.object({
  id: z.string().optional().default(''),
  name: z.string().optional().default(''),
  provider: z.string().optional().default(''),
  visibility: z.string().optional().default(''),
  status: z.string().optional().default(''),
  online: z.boolean().optional().default(false),
  last_seen_at: nullableIso,
  running_tasks: z.number().optional().default(0),
  stuck_tasks: z.number().optional().default(0),
  agents: z.array(DeploymentFleetAgentSchema).optional().default([]),
});

export const DeploymentFleetMachineSchema = z.object({
  daemon_id: z.string().optional().default(''),
  workspace_id: z.string().optional().default(''),
  workspace_name: z.string().optional().default(''),
  workspace_slug: z.string().optional().default(''),
  owner_email: z.string().optional().default(''),
  owner_name: z.string().optional().default(''),
  device_info: z.string().optional().default(''),
  online: z.boolean().optional().default(false),
  last_heartbeat_at: nullableIso,
  client_version: z.string().optional().default(''),
  version_outdated: z.boolean().optional().default(false),
  running_tasks: z.number().optional().default(0),
  stuck_tasks: z.number().optional().default(0),
  runtimes: z.array(DeploymentFleetRuntimeSchema).optional().default([]),
});

export const DeploymentFleetSummarySchema = z.object({
  machines_total: z.number().optional().default(0),
  machines_online: z.number().optional().default(0),
  machines_offline: z.number().optional().default(0),
  outdated_versions: z.number().optional().default(0),
  running_tasks: z.number().optional().default(0),
  stuck_tasks: z.number().optional().default(0),
});

export const DeploymentFleetSchema = z.object({
  machines: z.array(DeploymentFleetMachineSchema).optional().default([]),
  summary: DeploymentFleetSummarySchema.optional().default(() =>
    DeploymentFleetSummarySchema.parse({}),
  ),
  total: z.number().optional().default(0),
  truncated: z.boolean().optional().default(false),
  min_client_version: z.string().optional().default(''),
});

export const EMPTY_DEPLOYMENT_FLEET: DeploymentFleet = {
  machines: [],
  summary: {
    machines_total: 0,
    machines_online: 0,
    machines_offline: 0,
    outdated_versions: 0,
    running_tasks: 0,
    stuck_tasks: 0,
  },
  total: 0,
  truncated: false,
  min_client_version: '',
};
