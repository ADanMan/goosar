'use client';

import { useState } from 'react';
import { CloudWaitlistExpand } from '@goosar/views/onboarding';
import { useLocale } from '../../i18n';

export function CloudSection() {
  const { t } = useLocale();
  const d = t.download.cloud;
  const [submitted, setSubmitted] = useState(false);

  return (
    <section className="bg-background py-20 text-foreground sm:py-24">
      <div className="mx-auto max-w-[720px] px-4 sm:px-6 lg:px-8">
        <h2 className="font-[family-name:var(--font-serif)] text-xl leading-[1.1] tracking-[-0.03em] sm:text-2xl">
          {d.title}
        </h2>
        <p className="mt-4 max-w-[560px] text-sm leading-7 text-muted-foreground">{d.sub}</p>

        <div className="mt-10">
          <CloudWaitlistExpand
            submitted={submitted}
            onSubmitted={() => setSubmitted(true)}
            context="download"
          />
        </div>
      </div>
    </section>
  );
}
