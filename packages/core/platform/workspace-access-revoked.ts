// Хук платформы «пользователь только что потерял доступ к воркспейсу».

type WorkspaceAccessRevokedHandler = (workspaceId: string) => void;

let handler: WorkspaceAccessRevokedHandler | null = null;

export function registerWorkspaceAccessRevokedHandler(
  next: WorkspaceAccessRevokedHandler | null,
): void {
  handler = next;
}

export function notifyWorkspaceAccessRevoked(workspaceId: string): void {
  if (!workspaceId) return;
  handler?.(workspaceId);
}
