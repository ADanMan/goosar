// Сброс фильтров при смене активного воркспейса между двумя определёнными
// значениями; первый рендер пропускается.
import { useEffect, useRef } from 'react';

export function useClearFiltersOnWorkspaceChange(clearFn: () => void, wsId: string | null) {
  const prevRef = useRef<string | null>(null);
  useEffect(() => {
    if (prevRef.current && wsId && wsId !== prevRef.current) {
      clearFn();
    }
    prevRef.current = wsId ?? null;
  }, [wsId, clearFn]);
}
