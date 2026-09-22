import type { Workspace } from '../types';
import { useAuthStore } from '../auth';
import { paths } from './paths';

export function resolvePostAuthDestination(workspaces: Workspace[], hasOnboarded: boolean): string {
  if (!hasOnboarded) {
    return paths.onboarding();
  }
  const first = workspaces[0];
  if (first) {
    return paths.workspace(first.slug).issues();
  }
  return paths.newWorkspace();
}

export function useHasOnboarded(): boolean {
  return useAuthStore((s) => s.user?.onboarded_at != null);
}
