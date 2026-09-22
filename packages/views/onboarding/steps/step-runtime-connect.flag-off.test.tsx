import { describe, expect, it, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { AgentRuntime } from '@goosar/core/types';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enOnboarding from '../../locales/en/onboarding.json';

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

vi.mock('@goosar/core/runtimes', async (importActual) => ({
  ...(await importActual<typeof import('@goosar/core/runtimes')>()),
  ONBOARDING_HERMES_ONLY: false,
}));

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

function makeRuntime(id: string, provider: string, name: string): AgentRuntime {
  return {
    id,
    name,
    provider,
    status: 'online',
    runtime_mode: 'local',
    daemon_id: 'daemon-local',
    device_info: '',
    metadata: {},
    last_seen_at: new Date().toISOString(),
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  } as AgentRuntime;
}

function renderStep() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <StepRuntimeConnect wsId="ws_test" onNext={vi.fn()} onBack={vi.fn()} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe('StepRuntimeConnect with ONBOARDING_HERMES_ONLY off', () => {
  it('renders the multi-provider runtime grid instead of the machine view', () => {
    const claude = makeRuntime('rt_claude', 'claude', 'Claude (dev-box)');
    const codex = makeRuntime('rt_codex', 'codex', 'Codex (dev-box)');
    mocks.pickerState.runtimes = [claude, codex];
    mocks.pickerState.selected = claude;
    mocks.pickerState.selectedId = claude.id;
    mocks.pickerState.hasRuntimes = true;

    renderStep();

    expect(screen.getByText(/pick an agent runtime/i)).toBeInTheDocument();

    const group = screen.getByRole('radiogroup', { name: /agent runtimes/i });
    expect(within(group).getAllByRole('radio')).toHaveLength(2);

    expect(screen.queryByRole('radiogroup', { name: /your computers/i })).toBeNull();
    expect(screen.queryByText(/^this computer$/i)).toBeNull();
  });
});
