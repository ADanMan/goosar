'use client';

import { DashboardLayout } from '@goosar/views/layout';
import { GoosarIcon } from '@goosar/ui/components/common/goosar-icon';
import { SearchCommand, SearchTrigger } from '@goosar/views/search';
import { FloatingChat } from '@goosar/views/chat';
import { WebNotificationBridge } from '@/components/web-notification-bridge';

export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <DashboardLayout
      loadingIndicator={<GoosarIcon className="size-10" />}
      searchSlot={<SearchTrigger />}
      extra={
        <>
          <SearchCommand />
          <WebNotificationBridge />
          <FloatingChat />
        </>
      }
    >
      {children}
    </DashboardLayout>
  );
}
