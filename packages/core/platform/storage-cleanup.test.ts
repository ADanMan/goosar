import { describe, it, expect, vi, beforeEach } from 'vitest';
import { clearWorkspaceStorage } from './storage-cleanup';
import {
  registerDraftCleanup,
  __clearDraftCleanupRegistryForTest,
} from '../drafts/cleanup-registry';

beforeEach(() => {
  __clearDraftCleanupRegistryForTest();
});

describe('clearWorkspaceStorage', () => {
  it('removes all non-draft workspace-scoped keys for the given slug', () => {
    const adapter = {
      getItem: vi.fn(),
      setItem: vi.fn(),
      removeItem: vi.fn(),
    };

    clearWorkspaceStorage(adapter, 'ws_123');

    expect(adapter.removeItem).toHaveBeenCalledWith('goosar_issue_surface_views:ws_123');
    expect(adapter.removeItem).toHaveBeenCalledWith('goosar_issues_view:ws_123');
    expect(adapter.removeItem).toHaveBeenCalledWith('goosar_issues_scope:ws_123');
    expect(adapter.removeItem).toHaveBeenCalledWith('goosar_my_issues_view:ws_123');
    expect(adapter.removeItem).toHaveBeenCalledWith('goosar:chat:selectedAgentId:ws_123');
    expect(adapter.removeItem).toHaveBeenCalledWith('goosar:chat:selectedProjectId:ws_123');
    expect(adapter.removeItem).toHaveBeenCalledWith('goosar:chat:activeSessionId:ws_123');
    expect(adapter.removeItem).toHaveBeenCalledWith('goosar:chat:expanded:ws_123');
    expect(adapter.removeItem).toHaveBeenCalledWith('goosar_navigation:ws_123');
    expect(adapter.removeItem).toHaveBeenCalledTimes(9);
  });

  it('also clears registered draft keys via the registry', () => {
    const adapter = {
      getItem: vi.fn(),
      setItem: vi.fn(),
      removeItem: vi.fn(),
    };
    registerDraftCleanup({
      storageKey: 'goosar_test_draft',
      workspaceScoped: true,
      resetInMemory: vi.fn(),
    });
    registerDraftCleanup({
      storageKey: 'goosar_test_global_draft',
      workspaceScoped: false,
      resetInMemory: vi.fn(),
    });

    clearWorkspaceStorage(adapter, 'ws_123');

    expect(adapter.removeItem).toHaveBeenCalledWith('goosar_test_draft:ws_123');
    expect(adapter.removeItem).toHaveBeenCalledWith('goosar_test_global_draft');
    expect(adapter.removeItem).toHaveBeenCalledTimes(11);
  });
});
