export type MemberRole = 'owner' | 'admin' | 'member';

export interface WorkspaceRepo {
  url: string;
  description?: string;
}

export interface Workspace {
  id: string;
  name: string;
  slug: string;
  description: string | null;
  context: string | null;
  settings: Record<string, unknown>;
  repos: WorkspaceRepo[];
  issue_prefix: string;
  avatar_url: string | null;
  created_at: string;
  updated_at: string;
}

export interface WorkspaceTemplate {
  key: string;
  name: string;
  description: string;
}

export interface WorkspaceCapability {
  key: string;
  title: string;
  body: string;
}

export interface WorkspaceCapabilities {
  template_key: string;
  role_name: string;
  role_summary: string;
  capabilities: WorkspaceCapability[];
  sample_tasks: WorkspaceSampleTask[];
}

export interface WorkspaceSampleTask {
  key: string;
  title: string;
  prompt: string;
  requires: string[];
}

export interface Member {
  id: string;
  workspace_id: string;
  user_id: string;
  role: MemberRole;
  created_at: string;
  perimeter_access?: boolean;
}

export interface User {
  id: string;
  name: string;
  email: string;
  avatar_url: string | null;
  onboarded_at: string | null;
  onboarding_questionnaire: Record<string, unknown>;
  starter_content_state: string | null;
  language: string | null;
  profile_description: string;
  timezone: string | null;
  created_at: string;
  updated_at: string;
}

export interface MemberWithUser {
  id: string;
  workspace_id: string;
  user_id: string;
  role: MemberRole;
  created_at: string;
  name: string;
  email: string;
  avatar_url: string | null;
  perimeter_access?: boolean;
}

export interface Invitation {
  id: string;
  workspace_id: string;
  inviter_id: string;
  invitee_email: string;
  invitee_user_id: string | null;
  role: MemberRole;
  status: 'pending' | 'accepted' | 'declined' | 'expired';
  created_at: string;
  updated_at: string;
  expires_at: string;
  inviter_name?: string;
  inviter_email?: string;
  workspace_name?: string;
}
