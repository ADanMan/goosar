// Устранение шаблонного кода в realtime-хуках третьего слоя: подписки
// с автоматической отпиской.
import { useEffect } from 'react';
import type { WSClient } from '@/data/realtime/ws-client';
import { useWSClient } from '@/data/realtime/realtime-provider';
import { useWorkspaceStore } from '@/data/workspace-store';

export type WSSubscriptionSetup = (ws: WSClient, wsId: string) => (() => void)[] | undefined;

export function useWSSubscriptions(setup: WSSubscriptionSetup, deps: readonly unknown[]) {
  const ws = useWSClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  useEffect(() => {
    if (!ws || !wsId) return;
    const unsubs = setup(ws, wsId) ?? [];
    return () => {
      for (const u of unsubs) u();
    };
    // setup is intentionally NOT in deps — callers control re-subscription
    // via the explicit `deps` array. Putting setup in deps would re-fire
    // on every render (closures), defeating the whole point.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ws, wsId, ...deps]);
}
