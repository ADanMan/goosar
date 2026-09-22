// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, fireEvent, cleanup } from '@testing-library/react';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enAgents from '../../locales/en/agents.json';
import type { RuntimeMachine } from '../../runtimes/components/runtime-machines';
import { RuntimeMachineFilterDropdown } from './runtime-machine-filter-dropdown';

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

function makeMachine(overrides: Partial<RuntimeMachine> = {}): RuntimeMachine {
  return {
    id: 'machine-1',
    daemonId: 'daemon-1',
    title: 'dev.local',
    subtitle: 'x86_64 macOS',
    deviceInfo: 'dev.local · x86_64 macOS',
    cliVersion: '1.0.0',
    launchedBy: null,
    mode: 'local',
    section: 'local',
    isCurrent: true,
    health: 'online',
    runtimes: [],
    onlineCount: 1,
    issueCount: 0,
    runningCount: 0,
    queuedCount: 0,
    providerNames: ['claude'],
    lastSeenAt: '2026-05-17T11:59:50Z',
    ...overrides,
  };
}

function renderDropdown(
  machines: RuntimeMachine[],
  value: string | null,
  onChange: (id: string | null) => void,
  agentCountByMachine: Map<string, number>,
  totalAgentCount?: number,
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <RuntimeMachineFilterDropdown
          machines={machines}
          value={value}
          onChange={onChange}
          agentCountByMachine={agentCountByMachine}
          totalAgentCount={
            totalAgentCount ??
            Array.from(agentCountByMachine.values()).reduce((sum, n) => sum + n, 0)
          }
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe('RuntimeMachineFilterDropdown', () => {
  beforeEach(() => vi.clearAllMocks());
  afterEach(() => {
    cleanup();
    document.body.innerHTML = '';
  });

  it('shows the All-runtimes label and total scope count when nothing is selected', () => {
    const machines = [
      makeMachine({ id: 'm-local', title: 'dev.local' }),
      makeMachine({
        id: 'm-remote',
        title: 'build-server',
        section: 'remote',
        isCurrent: false,
      }),
    ];
    const counts = new Map([
      ['m-local', 2],
      ['m-remote', 5],
    ]);

    renderDropdown(machines, null, vi.fn(), counts);

    const trigger = screen.getByTestId('agents-runtime-filter');
    expect(trigger.textContent).toContain('All runtimes');
    expect(trigger.textContent).toContain('7');
  });

  it("shows the selected machine's title and per-machine count in the trigger", () => {
    const machines = [makeMachine({ id: 'm-local', title: 'dev.local' })];
    const counts = new Map([['m-local', 4]]);

    renderDropdown(machines, 'm-local', vi.fn(), counts);

    const trigger = screen.getByTestId('agents-runtime-filter');
    expect(trigger.textContent).toContain('dev.local');
    expect(trigger.textContent).toContain('4');
  });

  it('groups machines under their section headers in the menu', () => {
    const machines = [
      makeMachine({ id: 'm-local', title: 'dev.local', section: 'local' }),
      makeMachine({
        id: 'm-remote',
        title: 'build-server',
        section: 'remote',
        isCurrent: false,
      }),
      makeMachine({
        id: 'm-cloud',
        title: 'Goosar cloud',
        section: 'cloud',
        isCurrent: false,
        mode: 'cloud',
      }),
    ];
    const counts = new Map([
      ['m-local', 1],
      ['m-remote', 2],
      ['m-cloud', 3],
    ]);

    renderDropdown(machines, null, vi.fn(), counts);

    fireEvent.click(screen.getByTestId('agents-runtime-filter'));

    expect(screen.getByText('Local')).toBeTruthy();
    expect(screen.getByText('Remote')).toBeTruthy();
    expect(screen.getByText('Cloud')).toBeTruthy();
    expect(screen.getByText('dev.local')).toBeTruthy();
    expect(screen.getByText('build-server')).toBeTruthy();
    expect(screen.getByText('Goosar cloud')).toBeTruthy();
  });

  it('fires onChange(null) when the All-runtimes row is clicked', () => {
    const machines = [makeMachine({ id: 'm-local', title: 'dev.local' })];
    const counts = new Map([['m-local', 1]]);
    const onChange = vi.fn();

    renderDropdown(machines, 'm-local', onChange, counts);
    fireEvent.click(screen.getByTestId('agents-runtime-filter'));
    const activeRow = screen.getByTestId('agents-runtime-filter-active');
    expect(activeRow.getAttribute('role')).toBe('menuitem');
    fireEvent.click(screen.getByRole('menuitem', { name: /All runtimes/ }));
    expect(onChange).toHaveBeenCalledWith(null);
  });

  it('fires onChange(machineId) when a specific machine row is clicked', () => {
    const machines = [
      makeMachine({ id: 'm-local', title: 'dev.local', section: 'local' }),
      makeMachine({
        id: 'm-remote',
        title: 'build-server',
        section: 'remote',
        isCurrent: false,
      }),
    ];
    const counts = new Map([
      ['m-local', 1],
      ['m-remote', 2],
    ]);
    const onChange = vi.fn();

    renderDropdown(machines, null, onChange, counts);
    fireEvent.click(screen.getByTestId('agents-runtime-filter'));
    fireEvent.click(screen.getByRole('menuitem', { name: /build-server/ }));
    expect(onChange).toHaveBeenCalledWith('m-remote');
  });

  it('registers machine rows as menu items so they participate in keyboard nav / ARIA', () => {
    const machines = [makeMachine({ id: 'm-local', title: 'dev.local' })];
    const counts = new Map([['m-local', 1]]);

    renderDropdown(machines, null, vi.fn(), counts);
    fireEvent.click(screen.getByTestId('agents-runtime-filter'));

    const menu = screen.getByRole('menu');
    expect(menu).toBeTruthy();
    const items = screen.getAllByRole('menuitem');
    expect(items.length).toBeGreaterThanOrEqual(2);
    expect(items.every((item) => item.getAttribute('role') === 'menuitem')).toBe(true);
  });

  it('shows the per-machine count next to each item', () => {
    const machines = [makeMachine({ id: 'm-local', title: 'dev.local' })];
    const counts = new Map([['m-local', 7]]);

    renderDropdown(machines, null, vi.fn(), counts);
    fireEvent.click(screen.getByTestId('agents-runtime-filter'));

    const item = screen.getByRole('menuitem', { name: /dev.local/ });
    expect(item.textContent).toMatch(/7/);
  });

  it('renders an empty-state hint when no machines exist', () => {
    renderDropdown([], null, vi.fn(), new Map());

    fireEvent.click(screen.getByTestId('agents-runtime-filter'));

    expect(screen.getByText('No machines yet')).toBeTruthy();
  });

  it('uses the explicit totalAgentCount for the All-runtimes badge even when it diverges from the per-machine sum', () => {
    const machines = [makeMachine({ id: 'm-local', title: 'dev.local' })];
    const counts = new Map([['m-local', 3]]);

    renderDropdown(machines, null, vi.fn(), counts, /* totalAgentCount */ 5);

    const trigger = screen.getByTestId('agents-runtime-filter');
    expect(trigger.textContent).toContain('5');
  });
});
