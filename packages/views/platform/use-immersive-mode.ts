import { useEffect } from 'react';

type ImmersiveCapableAPI = {
  setImmersiveMode?: (immersive: boolean) => Promise<void> | void;
};

function getDesktopAPI(): ImmersiveCapableAPI | undefined {
  if (typeof window === 'undefined') return undefined;
  return (window as unknown as { desktopAPI?: ImmersiveCapableAPI }).desktopAPI;
}

export function useImmersiveMode(enabled: boolean = true): void {
  useEffect(() => {
    if (!enabled) return;
    const api = getDesktopAPI();
    api?.setImmersiveMode?.(true);
    return () => {
      api?.setImmersiveMode?.(false);
    };
  }, [enabled]);
}
