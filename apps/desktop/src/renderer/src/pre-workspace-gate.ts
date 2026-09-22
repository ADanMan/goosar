// Что главное окно должно показать пользователю до отрисовки дашборда.
export type PreWorkspaceGate = 'dashboard' | 'new-workspace' | 'onboarding' | 'check-invitations';

export function resolvePreWorkspaceGate({
  hasOnboarded,
  workspaceCount,
  hasPendingCompletion,
}: {
  hasOnboarded: boolean;
  workspaceCount: number;
  hasPendingCompletion: boolean;
}): PreWorkspaceGate {
  if (hasOnboarded) {
    return workspaceCount > 0 ? 'dashboard' : 'new-workspace';
  }
  return hasPendingCompletion ? 'onboarding' : 'check-invitations';
}
