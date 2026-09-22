import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';

import type { DaemonStatus } from '../../../shared/daemon-types';

vi.mock('@tanstack/react-query', () => ({
  useQuery: () => ({ data: [] }),
}));
vi.mock('@goosar/core/hooks', () => ({
  useWorkspaceId: () => 'ws-1',
}));
vi.mock('@goosar/core/runtimes', () => ({
  runtimeListOptions: () => ({ queryKey: ['runtimes'] }),
}));
vi.mock('@goosar/core/agents', () => ({
  agentTaskSnapshotOptions: () => ({ queryKey: ['snapshot'] }),
}));
vi.mock('./daemon-panel', () => ({ DaemonPanel: () => null }));
vi.mock('../platform/daemon-reauth', () => ({
  reauthenticateDaemon: vi.fn(),
}));
vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

vi.mock('@goosar/views/i18n', async () => {
  const { settingsI18nMock } = await import('../../../../test/i18n-settings-mock');
  return await settingsI18nMock();
});

import { DaemonRuntimeActions } from './daemon-runtime-card';

function stubDaemonAPI(status: DaemonStatus) {
  Object.defineProperty(window, 'daemonAPI', {
    configurable: true,
    value: {
      getStatus: vi.fn().mockResolvedValue(status),
      onStatusChange: vi.fn(() => () => {}),
    },
  });
}

describe('DaemonRuntimeActions — externally managed daemon (#3916)', () => {
  it("hides Stop/Restart and shows the managed-outside hint for a daemon the app can't control", async () => {
    stubDaemonAPI({ state: 'running', daemonId: 'd1', externallyManaged: true });
    render(<DaemonRuntimeActions />);

    expect(await screen.findByText('View logs')).toBeInTheDocument();
    expect(screen.getByText('Managed outside the app')).toBeInTheDocument();
    expect(screen.queryByText('Restart')).not.toBeInTheDocument();
    expect(screen.queryByText('Stop')).not.toBeInTheDocument();
  });

  it('shows Stop/Restart for a normally-managed running daemon (no 误伤)', async () => {
    stubDaemonAPI({
      state: 'running',
      daemonId: 'd1',
      externallyManaged: false,
    });
    render(<DaemonRuntimeActions />);

    expect(await screen.findByText('Restart')).toBeInTheDocument();
    expect(screen.getByText('Stop')).toBeInTheDocument();
    expect(screen.queryByText('Managed outside the app')).not.toBeInTheDocument();
  });
});
