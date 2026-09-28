'use client';

import Link from 'next/link';
import { useLocale } from '../i18n';
import { GitHubMark, githubUrl, heroButtonClassName } from './shared';

export function OpenSourceSection() {
  const { t } = useLocale();

  return (
    <section id="open-source" className="bg-background text-foreground">
      <div className="mx-auto max-w-[1320px] px-4 py-24 sm:px-6 sm:py-32 lg:px-8 lg:py-40">
        <div className="flex flex-col gap-16 lg:flex-row lg:items-start lg:gap-24">
          {/* Left column — heading + CTA */}
          <div className="lg:w-[480px] lg:shrink-0">
            <p className="text-2xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">
              {t.openSource.label}
            </p>
            <h2 className="mt-4 font-[family-name:var(--font-serif)] text-xl leading-[1.05] tracking-[-0.03em] sm:text-2xl">
              {t.openSource.headlineLine1}
              <br />
              {t.openSource.headlineLine2}
            </h2>
            <p className="mt-6 max-w-[420px] text-sm leading-7 text-muted-foreground sm:text-base">
              {t.openSource.description}
            </p>
            <div className="mt-8 flex flex-wrap items-center gap-3">
              <Link href={githubUrl} target="_blank" rel="noreferrer" className={heroButtonClassName('solid')}>
                <GitHubMark className="size-4" />
                {t.openSource.cta}
              </Link>
            </div>
          </div>

          {/* Right column — highlight grid */}
          <div className="flex-1">
            <div className="grid gap-px overflow-hidden rounded-2xl border border-border bg-border sm:grid-cols-2">
              {t.openSource.highlights.map((item) => (
                <div key={item.title} className="bg-card p-8 lg:p-10">
                  <h3 className="text-base font-semibold leading-snug text-foreground sm:text-lg">
                    {item.title}
                  </h3>
                  <p className="mt-3 text-sm leading-[1.7] text-muted-foreground">
                    {item.description}
                  </p>
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
