'use client';

import Link from 'next/link';
import { useAuthStore } from '@goosar/core/auth';
import { useLocale } from '../i18n';
import { useDashboardCtaHref } from '../utils/use-dashboard-cta';
import { GitHubMark, githubUrl, heroButtonClassName } from './shared';

export function HowItWorksSection() {
  const { t } = useLocale();
  const user = useAuthStore((s) => s.user);
  const ctaHref = useDashboardCtaHref();

  return (
    <section id="how-it-works" className="bg-inverse text-inverse-foreground">
      <div className="mx-auto max-w-[1320px] px-4 py-24 sm:px-6 sm:py-32 lg:px-8 lg:py-40">
        <p className="text-2xs font-semibold uppercase tracking-[0.16em] text-inverse-muted-foreground">
          {t.howItWorks.label}
        </p>
        <h2 className="mt-4 font-[family-name:var(--font-serif)] text-xl leading-[1.05] tracking-[-0.03em] sm:text-2xl">
          {t.howItWorks.headlineMain}
          <br />
          <span className="text-inverse-muted-foreground">{t.howItWorks.headlineFaded}</span>
        </h2>

        <div className="mt-20 grid gap-px overflow-hidden rounded-2xl border border-inverse-foreground/15 bg-inverse-foreground/15 sm:grid-cols-2 lg:grid-cols-4">
          {t.howItWorks.steps.map((step, i) => (
            <div key={i} className="flex flex-col bg-inverse p-8 lg:p-10">
              <span className="text-xs font-semibold tabular-nums text-inverse-muted-foreground">
                {String(i + 1).padStart(2, '0')}
              </span>
              <h3 className="mt-4 text-base font-semibold leading-snug text-inverse-foreground sm:text-lg">
                {step.title}
              </h3>
              <p className="mt-3 text-sm leading-[1.7] text-inverse-muted-foreground">
                {step.description}
              </p>
            </div>
          ))}
        </div>

        <div className="mt-14 flex flex-wrap items-center gap-4">
          <Link href={ctaHref} className={heroButtonClassName('solid')}>
            {user ? t.header.dashboard : t.howItWorks.cta}
          </Link>
          <Link
            href={githubUrl}
            target="_blank"
            rel="noreferrer"
            className={heroButtonClassName('ghost')}
          >
            <GitHubMark className="size-4" />
            {t.howItWorks.ctaGithub}
          </Link>
        </div>
      </div>
    </section>
  );
}
