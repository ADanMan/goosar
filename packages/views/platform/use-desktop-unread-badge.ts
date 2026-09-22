import { useEffect } from 'react';
import { useInboxUnreadCount } from '@goosar/core/inbox/queries';

type BadgeCapableAPI = {
  setUnreadBadge?: (count: number) => void;
};

function getDesktopAPI(): BadgeCapableAPI | undefined {
  if (typeof window === 'undefined') return undefined;
  return (window as unknown as { desktopAPI?: BadgeCapableAPI }).desktopAPI;
}

export function useDesktopUnreadBadge(wsId: string | null | undefined): void {
  const count = useInboxUnreadCount(wsId);
  useEffect(() => {
    getDesktopAPI()?.setUnreadBadge?.(count);
  }, [count]);
}
