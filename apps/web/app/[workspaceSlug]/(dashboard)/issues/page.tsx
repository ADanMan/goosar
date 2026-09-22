'use client';

import { IssuesPage } from '@goosar/views/issues/components';
import { ErrorBoundary } from '@goosar/ui/components/common/error-boundary';

export default function Page() {
  return (
    <ErrorBoundary>
      <IssuesPage />
    </ErrorBoundary>
  );
}
