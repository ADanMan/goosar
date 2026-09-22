'use client';

import { Suspense } from 'react';
import { useSearchParams } from 'next/navigation';
import { SlackBindPage } from '@goosar/views/slack';

function SlackBindPageContent() {
  const searchParams = useSearchParams();
  const token = searchParams.get('token');
  return <SlackBindPage token={token} />;
}

export default function Page() {
  return (
    <Suspense fallback={null}>
      <SlackBindPageContent />
    </Suspense>
  );
}
