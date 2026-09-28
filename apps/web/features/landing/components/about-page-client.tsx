'use client';

import Link from 'next/link';
import { LandingHeader } from './landing-header';
import { LandingFooter } from './landing-footer';
import { GitHubMark, githubUrl, heroButtonClassName } from './shared';
import { useLocale } from '../i18n';

export function AboutPageClient() {
  const { t } = useLocale();

  return (
    <>
      <LandingHeader variant="light" />
      <main className="bg-background text-foreground">
        <div className="mx-auto max-w-xl px-4 py-16 sm:px-6 sm:py-20 lg:py-24">
          <h1 className="font-[family-name:var(--font-serif)] text-xl leading-[1.05] tracking-[-0.03em] sm:text-2xl">
            {t.about.title}
          </h1>
          <div className="mt-8 space-y-6 text-sm leading-[1.8] text-muted-foreground sm:text-base">
            {t.about.paragraphs.map((p, i) => (
              <p key={i}>{p}</p>
            ))}
          </div>

          <div className="mt-12">
            <Link href={githubUrl} target="_blank" rel="noreferrer" className={heroButtonClassName('solid')}>
              <GitHubMark className="size-4" />
              {t.about.cta}
            </Link>
          </div>
        </div>
      </main>
      <LandingFooter />
    </>
  );
}
