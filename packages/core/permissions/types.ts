import type { MemberRole } from '../types';

export interface PermissionContext {
  userId: string | null;
  role: MemberRole | null;
}

export type DecisionReason =
  | 'allowed'
  | 'not_authenticated'
  | 'not_member'
  | 'not_owner_role'
  | 'not_admin_role'
  | 'not_resource_owner'
  | 'last_owner'
  | 'owner_grant_protected'
  | 'private_visibility'
  | 'unknown';

export interface Decision {
  allowed: boolean;
  reason: DecisionReason;
  message: string;
}

export const ALLOW: Decision = {
  allowed: true,
  reason: 'allowed',
  message: '',
};

export function deny(reason: DecisionReason, message: string): Decision {
  return { allowed: false, reason, message };
}
