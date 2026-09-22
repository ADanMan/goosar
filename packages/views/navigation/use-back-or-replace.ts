'use client';

import { useCallback } from 'react';
import { useNavigation } from './context';

export function useBackOrReplace(): (fallback: string) => void {
  const { back, replace, canGoBack } = useNavigation();
  return useCallback(
    (fallback: string) => {
      if (canGoBack?.() === true) back();
      else replace(fallback);
    },
    [back, replace, canGoBack],
  );
}
