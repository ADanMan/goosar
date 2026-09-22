'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useWSEvent } from '@goosar/core/realtime';
import { runtimeKeys, runtimeListOptions } from '@goosar/core/runtimes/queries';
import { filterRuntimesForOnboarding, sortRuntimesForOnboarding } from '@goosar/core/runtimes';
import type { AgentRuntime } from '@goosar/core/types';

export function onboardingRuntimePollInterval(data: AgentRuntime[] | undefined): number | false {
  return filterRuntimesForOnboarding(data ?? []).length > 0 ? false : 2000;
}

export function useRuntimePicker(wsId: string): {
  runtimes: AgentRuntime[];
  selected: AgentRuntime | null;
  selectedId: string | null;
  setSelectedId: (id: string) => void;
  hasRuntimes: boolean;
} {
  const qc = useQueryClient();

  const { data: unsortedRuntimes = [] } = useQuery({
    ...runtimeListOptions(wsId, 'me'),
    refetchInterval: (q) => onboardingRuntimePollInterval(q.state.data),
  });

  const runtimes = useMemo(
    () => sortRuntimesForOnboarding(filterRuntimesForOnboarding(unsortedRuntimes)),
    [unsortedRuntimes],
  );

  const handleDaemonEvent = useCallback(() => {
    qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
  }, [qc, wsId]);
  useWSEvent('daemon:register', handleDaemonEvent);

  const [selectedId, setSelectedId] = useState<string | null>(null);

  useEffect(() => {
    if (selectedId) return;
    const preferred = runtimes.find((r) => r.status === 'online') ?? runtimes[0];
    if (preferred) setSelectedId(preferred.id);
  }, [runtimes, selectedId]);

  const selected = runtimes.find((r) => r.id === selectedId) ?? null;

  return {
    runtimes,
    selected,
    selectedId,
    setSelectedId,
    hasRuntimes: runtimes.length > 0,
  };
}
