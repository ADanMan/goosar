'use client';

import { useMemo } from 'react';
import { useOptionalNavigation } from './context';

export function useAppOrigin(): string | null {
  const navigation = useOptionalNavigation();
  const getShareableUrl = navigation?.getShareableUrl;
  return useMemo(() => {
    if (!getShareableUrl) return null;
    try {
      return new URL(getShareableUrl('/')).origin;
    } catch {
      return null;
    }
  }, [getShareableUrl]);
}
