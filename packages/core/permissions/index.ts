// Публичный API модуля permissions: экспортируется только то, что используют
// views; полный набор правил — в ./rules.
export type { Decision, DecisionReason, PermissionContext } from './types';

export {
  canAssignAgentToIssue,
  canEditAgent,
  canGrantPerimeterAccess,
  canManageMembers,
} from './rules';

export { useAgentPermissions, useSkillPermissions } from './use-resource-permissions';

export { useCurrentMember } from './use-current-member';
