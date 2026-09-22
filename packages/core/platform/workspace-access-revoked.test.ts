// Тесты хука «пользователь потерял доступ к воркспейсу».

import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  notifyWorkspaceAccessRevoked,
  registerWorkspaceAccessRevokedHandler,
} from './workspace-access-revoked';

afterEach(() => {
  registerWorkspaceAccessRevokedHandler(null);
});

describe('workspace-access-revoked platform hook', () => {
  it('routes the notification to the registered handler', () => {
    const handler = vi.fn();
    registerWorkspaceAccessRevokedHandler(handler);
    notifyWorkspaceAccessRevoked('ws_1');
    expect(handler).toHaveBeenCalledExactlyOnceWith('ws_1');
  });

  it('is a silent no-op without a handler (web) and after unregistering', () => {
    expect(() => notifyWorkspaceAccessRevoked('ws_1')).not.toThrow();

    const handler = vi.fn();
    registerWorkspaceAccessRevokedHandler(handler);
    registerWorkspaceAccessRevokedHandler(null);
    notifyWorkspaceAccessRevoked('ws_1');
    expect(handler).not.toHaveBeenCalled();
  });

  it('ignores an empty workspace id', () => {
    const handler = vi.fn();
    registerWorkspaceAccessRevokedHandler(handler);
    notifyWorkspaceAccessRevoked('');
    expect(handler).not.toHaveBeenCalled();
  });
});
