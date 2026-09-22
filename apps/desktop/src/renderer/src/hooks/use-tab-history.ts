import { useCallback } from 'react';
import { useTabStore, useActiveTabHistory } from '@/stores/tab-store';

export function useTabHistory() {
  const { historyIndex, historyLength } = useActiveTabHistory();

  const canGoBack = historyIndex > 0;
  const canGoForward = historyIndex < historyLength - 1;

  const goBack = useCallback(() => {
    useTabStore.getState().goBack();
  }, []);

  const goForward = useCallback(() => {
    useTabStore.getState().goForward();
  }, []);

  return { canGoBack, canGoForward, goBack, goForward };
}
