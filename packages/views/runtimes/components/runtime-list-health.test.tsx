// @vitest-environment jsdom

import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import type { AgentRuntime } from '@goosar/core/types';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enRuntimes from '../../locales/en/runtimes.json';
import enAgents from '../../locales/en/agents.json';
import { CliCell, HealthCell } from './runtime-list';

const TEST_RESOURCES = {
  en: { common: enCommon, runtimes: enRuntimes, agents: enAgents },
};

const NOW = Date.now();
const EMPTY_WORKLOAD = { agentIds: [], runningCount: 0, queuedCount: 0 };

function makeRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: 'rt-1',
    workspace_id: 'ws-1',
    daemon_id: 'daemon-1',
    name: 'Claude (dev.local)',
    runtime_mode: 'local',
    provider: 'claude',
    launch_header: '',
    status: 'online',
    device_info: 'dev.local',
    metadata: {},
    owner_id: 'user-1',
    visibility: 'private',
    last_seen_at: new Date(NOW - 10_000).toISOString(),
    created_at: '2026-05-17T11:00:00Z',
    updated_at: '2026-05-17T11:00:00Z',
    ...overrides,
  };
}

function machineBoundElsewhere(): AgentRuntime {
  return makeRuntime({
    id: 'rt-custom-failed',
    name: 'Hermes (dev.local)',
    provider: 'codex',
    status: 'offline',
    profile_id: 'profile-1',
    last_seen_at: new Date(NOW - 1_000).toISOString(),
    metadata: {
      runtime_profile_registration_error: true,
      runtime_profile_failure_reason: 'command not found on PATH: hermes',
      command_name: 'hermes',
    },
  });
}

function renderCell(node: React.ReactNode) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {node}
    </I18nProvider>,
  );
}

describe('runtime list health column', () => {
  it('names a runtime bound to another machine instead of raising an error (#22)', () => {
    renderCell(
      <HealthCell runtime={machineBoundElsewhere()} workload={EMPTY_WORKLOAD} now={NOW} />,
    );

    expect(screen.getByText('Not available on this device')).toBeInTheDocument();
    expect(screen.queryByText('Registration error')).not.toBeInTheDocument();
    expect(screen.queryByText(/just now/)).not.toBeInTheDocument();
    expect(screen.queryByText(/^Online/)).not.toBeInTheDocument();
  });

  it("keeps the daemon's raw reason reachable without shouting it", () => {
    const { container } = renderCell(
      <HealthCell runtime={machineBoundElsewhere()} workload={EMPTY_WORKLOAD} now={NOW} />,
    );

    expect(container.querySelector('[title="command not found on PATH: hermes"]')).not.toBeNull();
    expect(container.querySelector('.text-destructive')).toBeNull();
  });

  it('still reports liveness for a runtime that really runs here', () => {
    renderCell(<HealthCell runtime={makeRuntime()} workload={EMPTY_WORKLOAD} now={NOW} />);

    expect(screen.getByText(/Online/)).toBeInTheDocument();
  });
});

describe('runtime list CLI column for a machine-bound runtime', () => {
  it('shows the command it would need without styling it as a failure', () => {
    const { container } = renderCell(<CliCell runtime={machineBoundElsewhere()} />);

    expect(screen.getByText('hermes')).toBeInTheDocument();
    expect(container.querySelector('.text-destructive')).toBeNull();
  });
});
