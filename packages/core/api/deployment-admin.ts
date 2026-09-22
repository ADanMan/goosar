import { z } from 'zod';
import { UserConfigOverrideViewSchema } from './workspace-admin';

export interface DeploymentAdminEntry {
  user_id: string;
  email?: string;
  name?: string;
  granted_by?: string;
  granted_at?: string;
}

export const DeploymentAdminEntrySchema = z.object({
  user_id: z.string().optional().default(''),
  email: z.string().optional(),
  name: z.string().optional(),
  granted_by: z.string().optional(),
  granted_at: z.string().optional(),
});

export const DeploymentAdminListSchema = z.array(DeploymentAdminEntrySchema);

export const EMPTY_DEPLOYMENT_ADMIN_LIST: DeploymentAdminEntry[] = [];

export interface DeploymentAdminPending {
  status: string;
  request_id: string;
  action: string;
  target_user_id: string;
  target_email?: string;
  requested_by?: string;
  requested_at?: string;
  confirm_hint: string;
}

export const DeploymentAdminPendingSchema = z.object({
  status: z.string().optional().default('pending'),
  request_id: z.string().optional().default(''),
  action: z.string().optional().default(''),
  target_user_id: z.string().optional().default(''),
  target_email: z.string().optional(),
  requested_by: z.string().optional(),
  requested_at: z.string().optional(),
  confirm_hint: z.string().optional().default(''),
});

export const EMPTY_DEPLOYMENT_ADMIN_PENDING: DeploymentAdminPending = {
  status: 'pending',
  request_id: '',
  action: '',
  target_user_id: '',
  confirm_hint: '',
};

export type DeploymentAdminAddResult =
  | { kind: 'already_admin'; entry: DeploymentAdminEntry }
  | { kind: 'pending'; pending: DeploymentAdminPending };

export const DeploymentAdminPendingListSchema = z.array(DeploymentAdminPendingSchema);

export const EMPTY_DEPLOYMENT_ADMIN_PENDING_LIST: DeploymentAdminPending[] = [];

export interface AdminAuditEntry {
  id: string;
  source?: string;
  actor_user_id?: string;
  actor_type?: string;
  actor_id?: string;
  actor_role?: string;
  action: string;
  target_type: string;
  target_id?: string;
  outcome?: string;
  reason?: string;
  before_hash?: string;
  after_hash?: string;
  workspace_id?: string;
  request_id?: string;
  client_ip?: string;
  user_agent?: string;
  created_at?: string;
  cursor?: string;
}

export const AdminAuditEntrySchema = z.object({
  id: z.string().optional().default(''),
  source: z.string().optional(),
  actor_user_id: z.string().optional(),
  actor_type: z.string().optional(),
  actor_id: z.string().optional(),
  actor_role: z.string().optional(),
  action: z.string().optional().default(''),
  target_type: z.string().optional().default(''),
  target_id: z.string().optional(),
  outcome: z.string().optional(),
  reason: z.string().optional(),
  before_hash: z.string().optional(),
  after_hash: z.string().optional(),
  workspace_id: z.string().optional(),
  request_id: z.string().optional(),
  client_ip: z.string().optional(),
  user_agent: z.string().optional(),
  created_at: z.string().optional(),
  cursor: z.string().optional(),
});

export interface DeploymentAuditQuery {
  limit?: number;
  action?: string;
  actor?: string;
  since?: string;
  until?: string;
  source?: 'admin' | 'auth' | 'all';
  cursor?: string;
}

export const AdminAuditListSchema = z.array(AdminAuditEntrySchema);

export const EMPTY_ADMIN_AUDIT_LIST: AdminAuditEntry[] = [];

export const MCP_POLICY_WILDCARD = '*';

export interface DeploymentPolicyLlm {
  base_url?: string;
  model?: string;
  locked?: boolean;
}

export interface DeploymentPolicyMcpEntry {
  enabled?: boolean;
  locked?: boolean;
}

export interface DeploymentPolicyDoc {
  llm?: DeploymentPolicyLlm;
  mcp?: Record<string, DeploymentPolicyMcpEntry>;
  session?: DeploymentPolicySession;
}

export interface DeploymentPolicySession {
  idle_timeout_hours?: number;
  absolute_lifetime_days?: number;
  max_concurrent_sessions?: number;
  require_mfa?: string;
}

const DeploymentPolicyLlmSchema = z.object({
  base_url: z.string().optional(),
  model: z.string().optional(),
  locked: z.boolean().optional(),
});

const DeploymentPolicyMcpEntrySchema = z.object({
  enabled: z.boolean().optional(),
  locked: z.boolean().optional(),
});

const DeploymentPolicySessionSchema = z.object({
  idle_timeout_hours: z.number().optional(),
  absolute_lifetime_days: z.number().optional(),
  max_concurrent_sessions: z.number().optional(),
  require_mfa: z.string().optional(),
});

export const DeploymentPolicyDocSchema = z.object({
  llm: DeploymentPolicyLlmSchema.optional(),
  mcp: z.record(z.string(), DeploymentPolicyMcpEntrySchema).optional(),
  session: DeploymentPolicySessionSchema.optional(),
});

export interface DeploymentPolicyView {
  policy: DeploymentPolicyDoc;
  updated_at?: string;
}

export const DeploymentPolicyViewSchema = z.object({
  policy: DeploymentPolicyDocSchema.nullish().transform((v): DeploymentPolicyDoc => v ?? {}),
  updated_at: z.string().optional(),
});

export const EMPTY_DEPLOYMENT_POLICY_VIEW: DeploymentPolicyView = {
  policy: {},
};

export function isMcpKillSwitchActive(doc: DeploymentPolicyDoc): boolean {
  const wildcard = doc.mcp?.[MCP_POLICY_WILDCARD];
  return wildcard?.enabled === false && wildcard?.locked === true;
}

export interface DeploymentWorkspaceEntry {
  id: string;
  name: string;
  slug: string;
  member_count: number;
}

export const DeploymentWorkspaceEntrySchema = z.object({
  id: z.string().optional().default(''),
  name: z.string().optional().default(''),
  slug: z.string().optional().default(''),
  member_count: z.number().optional().default(0),
});

export const DeploymentWorkspaceListSchema = z.array(DeploymentWorkspaceEntrySchema);

export const EMPTY_DEPLOYMENT_WORKSPACE_LIST: DeploymentWorkspaceEntry[] = [];

export interface DeploymentWorkspaceMemberEntry {
  user_id: string;
  name?: string;
  email?: string;
  role: string;
  deactivated: boolean;
}

export const DeploymentWorkspaceMemberEntrySchema = z.object({
  user_id: z.string().optional().default(''),
  name: z.string().optional(),
  email: z.string().optional(),
  role: z.string().optional().default(''),
  deactivated: z.boolean().optional().default(false),
});

export const DeploymentWorkspaceMemberListSchema = z.array(DeploymentWorkspaceMemberEntrySchema);

export const EMPTY_DEPLOYMENT_WORKSPACE_MEMBER_LIST: DeploymentWorkspaceMemberEntry[] = [];

export const DeploymentUserConfigOverrideListSchema = z.array(UserConfigOverrideViewSchema);

