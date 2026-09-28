'use client';

import { DashboardLayout } from '@goosar/views/layout';
import { GoosarIcon } from '@goosar/ui/components/common/goosar-icon';
import { SearchCommand } from '@goosar/views/search';
import { FloatingChat } from '@goosar/views/chat';
import { WebNotificationBridge } from '@/components/web-notification-bridge';

// Тестовая страница биллинга (T-032 §3.2) — тот же паттерн флага, что и
// NEXT_PUBLIC_ENABLE_CLOUD_RUNTIME для /runtimes. Флаг читается здесь (в
// web-приложении) и прокидывается в общий для web/desktop DashboardLayout
// пропом, чтобы пункт подпанели «Настройки» появлялся только когда сама
// страница /billing доступна.
const billingEnabled = process.env.NEXT_PUBLIC_ENABLE_BILLING_TEST_PAGE === 'true';

export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <DashboardLayout
      loadingIndicator={<GoosarIcon className="size-10" />}
      billingEnabled={billingEnabled}
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
