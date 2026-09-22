import { describe, it, expect } from 'vitest';

describe('useNavigationStore.lastPath excludes global paths', () => {
  it('does not persist /login, /workspaces/new, /invite/, /auth/, /logout, /signup', async () => {
    const { useNavigationStore } = await import('./store');
    const globalPrefixes = [
      '/login',
      '/logout',
      '/signup',
      '/workspaces/new',
      '/invite/abc',
      '/auth/callback',
    ];

    for (const path of globalPrefixes) {
      useNavigationStore.setState({ lastPath: '/sentinel' });
      useNavigationStore.getState().onPathChange(path);
      expect(
        useNavigationStore.getState().lastPath,
        `${path} should not be persisted as lastPath (would restore user to a global route)`,
      ).toBe('/sentinel');
    }
  });

  it('does persist workspace-scoped paths', async () => {
    const { useNavigationStore } = await import('./store');
    useNavigationStore.setState({ lastPath: null });
    useNavigationStore.getState().onPathChange('/acme/issues');
    expect(useNavigationStore.getState().lastPath).toBe('/acme/issues');
  });
});
