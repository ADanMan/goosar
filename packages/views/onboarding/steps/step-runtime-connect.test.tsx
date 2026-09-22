import type { ReactNode } from 'react';
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { AgentRuntime } from '@goosar/core/types';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enOnboarding from '../../locales/en/onboarding.json';

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

const mocks = vi.hoisted(() => ({
  pickerState: {
    runtimes: [] as AgentRuntime[],
    selected: null as AgentRuntime | null,
    selectedId: null as string | null,
    setSelectedId: vi.fn<(id: string) => void>(),
    hasRuntimes: false,
  },
}));

vi.mock('../components/use-runtime-picker', () => ({
  useRuntimePicker: () => mocks.pickerState,
}));

import { StepRuntimeConnect } from './step-runtime-connect';
import type { SaveLlmConnection } from '../components/llm-connection-form';

function makeRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: 'bee_1',
    name: 'hermes (dev-box)',
    provider: 'runtime-j',
    status: 'online',
    runtime_mode: 'local',
    daemon_id: 'daemon-local',
    device_info: '',
    metadata: {},
    last_seen_at: new Date().toISOString(),
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    ...overrides,
  } as AgentRuntime;
}

function renderStep(
  props: {
    runtimesPending?: boolean;
    onSaveLlmConnection?: SaveLlmConnection;
    localDaemonId?: string | null;
    localMachineName?: string | null;
    daemonState?: string | null;
    networkStatusSlot?: ReactNode;
  } = {},
) {
  const onNext = vi.fn();
  const onBack = vi.fn();
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <StepRuntimeConnect
          wsId="ws_test"
          onNext={onNext}
          onBack={onBack}
          runtimesPending={props.runtimesPending}
          onSaveLlmConnection={props.onSaveLlmConnection}
          localDaemonId={props.localDaemonId}
          localMachineName={props.localMachineName}
          daemonState={props.daemonState}
          networkStatusSlot={props.networkStatusSlot}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return { onNext, onBack };
}

function setPicker(runtime: AgentRuntime | null) {
  mocks.pickerState.runtimes = runtime ? [runtime] : [];
  mocks.pickerState.selected = runtime;
  mocks.pickerState.selectedId = runtime?.id ?? null;
  mocks.pickerState.hasRuntimes = runtime !== null;
}

describe('StepRuntimeConnect', () => {
  beforeEach(() => {
    setPicker(null);
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('mounts and shows the scanning UI without touching framework-level globals', () => {
    renderStep();
    expect(screen.getByText(/connecting this computer/i)).toBeInTheDocument();
  });

  it('does not render a permanently-disabled Start exploring while scanning', () => {
    renderStep();
    expect(screen.queryByRole('button', { name: /start exploring/i })).not.toBeInTheDocument();
  });

  it('flips to the hermes-not-found empty state after the idle timeout', () => {
    renderStep();
    act(() => vi.advanceTimersByTime(5000));
    expect(screen.getByText(/hermes not found on this computer/i)).toBeInTheDocument();
  });

  it('keeps scanning past the idle timeout while runtimes are pending, then falls back at the hard ceiling', () => {
    renderStep({ runtimesPending: true });

    act(() => vi.advanceTimersByTime(5000));
    expect(screen.getByText(/connecting this computer/i)).toBeInTheDocument();
    expect(screen.queryByText(/hermes not found/i)).not.toBeInTheDocument();

    act(() => vi.advanceTimersByTime(15000));
    expect(screen.getByText(/hermes not found/i)).toBeInTheDocument();
  });

  it('shows the connected machine — not the empty state — once the runtime registers', () => {
    setPicker(makeRuntime());

    renderStep();
    act(() => vi.advanceTimersByTime(25000));

    expect(screen.getByText(/this computer is connected/i)).toBeInTheDocument();
    expect(screen.queryByText(/hermes not found/i)).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /start exploring/i })).toBeInTheDocument();
  });

  it('labels the local daemon\'s machine "This computer" and shows its connection status', () => {
    setPicker(makeRuntime());

    renderStep({ localDaemonId: 'daemon-local', localMachineName: "Anna's Mac" });
    act(() => vi.advanceTimersByTime(1000));

    const card = screen.getByRole('radio', { name: /this computer/i });
    expect(card).toHaveAttribute('aria-checked', 'true');
    expect(screen.getByText(/anna's mac/i)).toBeInTheDocument();
    expect(screen.getByText(/^online$/i)).toBeInTheDocument();
  });

  it("keeps the machine title when the daemon is not this computer's", () => {
    setPicker(makeRuntime({ daemon_id: 'daemon-other' }));

    renderStep({ localDaemonId: 'daemon-local', localMachineName: "Anna's Mac" });
    act(() => vi.advanceTimersByTime(1000));

    expect(screen.queryByRole('radio', { name: /this computer/i })).not.toBeInTheDocument();
    expect(screen.getByRole('radio', { name: /dev-box/i })).toBeInTheDocument();
  });

  it('renders the LLM connection form when a hermes runtime is selected and the platform injected a saver', () => {
    setPicker(makeRuntime());

    renderStep({ onSaveLlmConnection: vi.fn() });

    expect(screen.getByLabelText(/access key/i)).toBeInTheDocument();
    expect(screen.getByRole('radio', { name: /^outside the perimeter$/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /start exploring/i })).toBeEnabled();
  });

  it('does not render the LLM connection form without an injected saver (web)', () => {
    setPicker(makeRuntime());

    renderStep();

    expect(screen.queryByLabelText(/access key/i)).not.toBeInTheDocument();
  });

  it('does not render the LLM connection form for a runtime the app does not configure', () => {
    setPicker(makeRuntime({ id: 'rt_1', name: 'Claude (dev-box)', provider: 'runtime-c' }));

    renderStep({ onSaveLlmConnection: vi.fn() });

    expect(screen.queryByLabelText(/access key/i)).not.toBeInTheDocument();
  });

  it('shows a single Skip affordance in the empty state (no duplicate footer button)', () => {
    renderStep();
    act(() => vi.advanceTimersByTime(5000));
    expect(screen.getAllByText('Skip for now')).toHaveLength(1);
  });
});

describe('empty state names the daemon reason (issue #438)', () => {
  beforeEach(() => {
    setPicker(null);
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('explains a daemon that is not running', () => {
    renderStep({ daemonState: 'stopped' });
    act(() => vi.advanceTimersByTime(5000));

    expect(screen.getByText(enOnboarding.step_runtime.empty_daemon_stopped)).toBeInTheDocument();
  });

  it('names the sign-in failure the daemon reports', () => {
    renderStep({ daemonState: 'auth_expired' });
    act(() => vi.advanceTimersByTime(5000));

    expect(
      screen.getByText(enOnboarding.step_runtime.empty_daemon_auth_expired),
    ).toBeInTheDocument();
  });

  it('renders a state this build has never heard of with the generic line', () => {
    renderStep({ daemonState: 'quarantined' });
    act(() => vi.advanceTimersByTime(5000));

    expect(screen.getByText(enOnboarding.step_runtime.empty_daemon_unknown)).toBeInTheDocument();
  });

  it('offers the network-status panel the shell injected', () => {
    renderStep({
      daemonState: 'stopped',
      networkStatusSlot: <button type="button">{'Network status'}</button>,
    });
    act(() => vi.advanceTimersByTime(5000));

    expect(
      screen.getByText(enOnboarding.step_runtime.empty_network_status_hint),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Network status' })).toBeInTheDocument();
  });

  it('says nothing extra when the platform reports no daemon state', () => {
    renderStep();
    act(() => vi.advanceTimersByTime(5000));

    expect(screen.queryByTestId('empty-daemon-reason')).toBeNull();
    expect(screen.queryByText(enOnboarding.step_runtime.empty_network_status_hint)).toBeNull();
  });
});
