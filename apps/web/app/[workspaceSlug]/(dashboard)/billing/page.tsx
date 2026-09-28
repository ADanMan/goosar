import { notFound } from 'next/navigation';
import { BillingTestPage } from '@goosar/views/billing';

// Тестовая страница биллинга (T-032 §3.2, Critical): не готовый интерфейс,
// просто все эндпоинты /api/v1/billing/* на одной странице для проверки
// прокси и Stripe-флоу. Раньше была доступна по прямому URL в проде без
// какого-либо флага — скрываем её за тем же паттерном, что и
// NEXT_PUBLIC_ENABLE_CLOUD_RUNTIME в runtimes/page.tsx.
const billingTestPageEnabled = process.env.NEXT_PUBLIC_ENABLE_BILLING_TEST_PAGE === 'true';

export default function BillingRoute() {
  if (!billingTestPageEnabled) notFound();
  return <BillingTestPage />;
}
