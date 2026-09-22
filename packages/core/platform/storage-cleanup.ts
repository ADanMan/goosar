import type { StorageAdapter } from '../types/storage';
import { clearRegisteredWorkspaceDrafts } from '../drafts/cleanup-registry';
import '../drafts/register-all-drafts';

const WORKSPACE_SCOPED_KEYS = [
  'goosar_issue_surface_views',
  'goosar_issues_view',
  'goosar_issues_scope',
  'goosar_my_issues_view',
  'goosar:chat:selectedAgentId',
  'goosar:chat:selectedProjectId',
  'goosar:chat:activeSessionId',
  'goosar:chat:expanded',
  'goosar_navigation',
];

export function clearWorkspaceStorage(adapter: StorageAdapter, slug: string) {
  for (const key of WORKSPACE_SCOPED_KEYS) {
    adapter.removeItem(`${key}:${slug}`);
  }
  clearRegisteredWorkspaceDrafts(adapter, slug);
}
