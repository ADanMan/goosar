import { describe, expect, it, vi } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { createElement, type ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { AgentRuntime } from '@goosar/core/types';

const mocks = vi.hoisted(() => ({
  listData: [] as AgentRuntime[],
}));

vi.mock('@goosar/core/realtime', () => ({
  useWSEvent: vi.fn(),
}));

vi.mock('@goosar/core/runtimes/queries', () => ({
  runtimeKeys: { all: (wsId: string) => ['runtimes', wsId] },
  runtimeListOptions: (wsId: string, scope: string) => ({
    queryKey: ['runtimes', wsId, scope],
    queryFn: async () => mocks.listData,
  }),
}));

import { onboardingRuntimePollInterval, useRuntimePicker } from './use-runtime-picker';

function makeRuntime(id: string, provider: string): AgentRuntime {
  return {
    id,
    name: `${provider} (dev-box)`,
    provider,
    status: 'online',
    runtime_mode: 'local',
    daemon_id: 'daemon-1',
    device_info: '',
    metadata: {},
    last_seen_at: new Date().toISOString(),
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  } as AgentRuntime;
}

describe('onboardingRuntimePollInterval', () => {
  it('keeps polling while the raw list is non-empty but the filtered list is empty', () => {
    expect(
      onboardingRuntimePollInterval([
        makeRuntime('claude-1', 'runtime-c'),
        makeRuntime('codex-1', 'runtime-e'),
      ]),
    ).toBe(2000);
  });

  it('stops polling once a hermes runtime is present', () => {
    expect(
      onboardingRuntimePollInterval([
        makeRuntime('claude-1', 'runtime-c'),
        makeRuntime('bee-1', 'runtime-j'),
      ]),
    ).toBe(false);
  });

  it('keeps polling on an empty or missing list', () => {
    expect(onboardingRuntimePollInterval([])).toBe(2000);
    expect(onboardingRuntimePollInterval(undefined)).toBe(2000);
  });
});

describe('useRuntimePicker', () => {
  function wrapper({ children }: { children: ReactNode }) {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    return createElement(QueryClientProvider, { client: qc }, children);
  }

  it('exposes only hermes runtimes and auto-selects one', async () => {
    mocks.listData = [makeRuntime('claude-1', 'runtime-c'), makeRuntime('bee-1', 'runtime-j')];

    const { result } = renderHook(() => useRuntimePicker('ws-1'), { wrapper });

    await waitFor(() => expect(result.current.hasRuntimes).toBe(true));
    expect(result.current.runtimes.map((r) => r.id)).toEqual(['bee-1']);
    expect(result.current.selectedId).toBe('bee-1');
  });

  it('reports no runtimes when the raw list has only other providers', async () => {
    mocks.listData = [makeRuntime('claude-1', 'runtime-c')];

    const { result } = renderHook(() => useRuntimePicker('ws-1'), { wrapper });

    await new Promise((resolve) => setTimeout(resolve, 25));
    expect(result.current.hasRuntimes).toBe(false);
    expect(result.current.runtimes).toEqual([]);
    expect(result.current.selected).toBeNull();
  });
});
