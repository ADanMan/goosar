import type { Agent, Comment, Member, MemberRole, RuntimeDevice, Skill } from '../types';
import { ALLOW, deny, type Decision, type PermissionContext } from './types';

const isAdminLike = (role: MemberRole | null) => role === 'owner' || role === 'admin';

export function canEditAgent(agent: Agent, ctx: PermissionContext): Decision {
  if (ctx.userId === null) {
    return deny('not_authenticated', 'Sign in to edit this agent.');
  }
  if (isAdminLike(ctx.role)) return ALLOW;
  if (agent.owner_id !== null && agent.owner_id === ctx.userId) return ALLOW;
  return deny(
    'not_resource_owner',
    'Only the agent owner and workspace admins can edit this agent.',
  );
}

export function canAssignAgentToIssue(agent: Agent, ctx: PermissionContext): Decision {
  if (ctx.userId === null) {
    return deny('not_authenticated', 'Sign in to assign agents.');
  }

  if (agent.owner_id !== null && agent.owner_id === ctx.userId) {
    return ALLOW;
  }

  if (agent.permission_mode === 'private') {
    return deny('private_visibility', 'Personal agent — only the owner can assign work.');
  }

  const targets = agent.invocation_targets ?? [];
  if (targets.some((t) => t.target_type === 'workspace')) {
    if (ctx.role === null) {
      return deny('not_member', 'Join this workspace to assign agents.');
    }
    return ALLOW;
  }

  if (targets.some((t) => t.target_type === 'member' && t.target_id === ctx.userId)) {
    return ALLOW;
  }

  return deny(
    'private_visibility',
    "Restricted agent — you don't have access to assign work to it.",
  );
}

export function canEditSkill(skill: Skill, ctx: PermissionContext): Decision {
  if (ctx.userId === null) {
    return deny('not_authenticated', 'Sign in to edit this skill.');
  }
  if (isAdminLike(ctx.role)) return ALLOW;
  if (skill.created_by !== null && skill.created_by === ctx.userId) {
    return ALLOW;
  }
  return deny('not_resource_owner', 'Only the creator and workspace admins can edit this skill.');
}

export function canDeleteSkill(skill: Skill, ctx: PermissionContext): Decision {
  return canEditSkill(skill, ctx);
}

export function canEditComment(comment: Comment, ctx: PermissionContext): Decision {
  if (ctx.userId === null) {
    return deny('not_authenticated', 'Sign in to edit comments.');
  }
  if (comment.author_type !== 'member') {
    return deny('not_resource_owner', 'Agent-authored comments cannot be edited.');
  }
  if (comment.author_id === ctx.userId) return ALLOW;
  if (isAdminLike(ctx.role)) return ALLOW;
  return deny('not_resource_owner', 'Only the author and workspace admins can edit this comment.');
}

export function canDeleteComment(comment: Comment, ctx: PermissionContext): Decision {
  if (ctx.userId === null) {
    return deny('not_authenticated', 'Sign in to delete comments.');
  }
  if (comment.author_type === 'member' && comment.author_id === ctx.userId) {
    return ALLOW;
  }
  if (isAdminLike(ctx.role)) return ALLOW;
  return deny(
    'not_resource_owner',
    'Only the author and workspace admins can delete this comment.',
  );
}

export function canDeleteRuntime(runtime: RuntimeDevice, ctx: PermissionContext): Decision {
  if (ctx.userId === null) {
    return deny('not_authenticated', 'Sign in to delete runtimes.');
  }
  if (isAdminLike(ctx.role)) return ALLOW;
  if (runtime.owner_id !== null && runtime.owner_id === ctx.userId) {
    return ALLOW;
  }
  return deny(
    'not_resource_owner',
    'Only the runtime owner and workspace admins can delete this runtime.',
  );
}

export function canUpdateWorkspaceSettings(ctx: PermissionContext): Decision {
  if (isAdminLike(ctx.role)) return ALLOW;
  return deny('not_admin_role', 'Only workspace owners and admins can update workspace settings.');
}

export function canDeleteWorkspace(ctx: PermissionContext): Decision {
  if (ctx.role === 'owner') return ALLOW;
  return deny('not_owner_role', 'Only the workspace owner can delete this workspace.');
}

export function canManageMembers(ctx: PermissionContext): Decision {
  if (isAdminLike(ctx.role)) return ALLOW;
  return deny('not_admin_role', 'Only workspace owners and admins can manage members.');
}

export function canChangeMemberRole(
  target: Pick<Member, 'role'>,
  ownerCount: number,
  ctx: PermissionContext,
): Decision {
  const manage = canManageMembers(ctx);
  if (!manage.allowed) return manage;

  if (target.role === 'owner') {
    if (ctx.role !== 'owner') {
      return deny('not_owner_role', "Only the workspace owner can change another owner's role.");
    }
    if (ownerCount <= 1) {
      return deny(
        'last_owner',
        'Promote another member to owner first — a workspace must keep at least one owner.',
      );
    }
  }
  return ALLOW;
}

export function canGrantPerimeterAccess(
  target: Pick<Member, 'role'>,
  ctx: PermissionContext,
): Decision {
  const manage = canManageMembers(ctx);
  if (!manage.allowed) return manage;

  if (target.role === 'owner' && ctx.role !== 'owner') {
    return deny(
      'owner_grant_protected',
      "Only the workspace owner can change another owner's perimeter access.",
    );
  }
  return ALLOW;
}
