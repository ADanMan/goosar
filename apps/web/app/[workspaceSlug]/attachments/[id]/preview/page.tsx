'use client';

import { use } from 'react';
import { useSearchParams } from 'next/navigation';
import { AttachmentPreviewPage } from '@goosar/views/attachments';
import { ErrorBoundary } from '@goosar/ui/components/common/error-boundary';

export default function AttachmentPreviewWebPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const search = useSearchParams();
  const filename = search.get('name') ?? undefined;

  return (
    <ErrorBoundary resetKeys={[id]}>
      <AttachmentPreviewPage attachmentId={id} filename={filename} />
    </ErrorBoundary>
  );
}
