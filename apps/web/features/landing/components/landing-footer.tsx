'use client';

import Link from 'next/link';
import { GoosarIcon } from '@goosar/ui/components/common/goosar-icon';
import { cn } from '@goosar/ui/lib/utils';
import { useAuthStore } from '@goosar/core/auth';
import { GitHubMark, githubUrl, heroButtonClassName } from './shared';
import { useLocale, locales, localeLabels } from '../i18n';
import { useDashboardCtaHref } from '../utils/use-dashboard-cta';

export function LandingFooter() {
  const { t, locale, setLocale } = useLocale();
  const user = useAuthStore((s) => s.user);
  const ctaHref = useDashboardCtaHref();
  const groups = Object.values(t.footer.groups);

  return (
    <footer className="bg-inverse text-inverse-foreground">
      <div className="mx-auto max-w-[1320px] px-4 sm:px-6 lg:px-8">
        {/* Top: CTA + link columns */}
        <div className="flex flex-col gap-12 border-b border-inverse-foreground/15 py-16 sm:py-20 lg:flex-row lg:gap-20">
          {/* Left — newsletter / CTA */}
          <div className="lg:w-[340px] lg:shrink-0">
            <Link href="/" className="flex items-center gap-3">
              <GoosarIcon className="size-10 text-inverse-foreground" noSpin />
              <span className="text-lg font-semibold tracking-[0.04em] lowercase">goosar</span>
            </Link>
            <p className="mt-4 max-w-[300px] text-sm leading-[1.7] text-inverse-muted-foreground">
              {t.footer.tagline}
            </p>
            <div className="mt-4 flex items-center gap-3">
              <Link
                href={githubUrl}
                target="_blank"
                rel="noreferrer"
                className="text-inverse-muted-foreground transition-colors hover:text-inverse-foreground"
              >
                <GitHubMark className="size-4" />
              </Link>
            </div>
            <div className="mt-6">
              <Link href={ctaHref} className={heroButtonClassName('solid')}>
                {user ? t.header.dashboard : t.footer.cta}
              </Link>
            </div>
          </div>

          {/* Right — link columns */}
          <div className="grid flex-1 grid-cols-2 gap-8 sm:grid-cols-4">
            {groups.map((group) => (
              <div key={group.label}>
                <h4 className="text-xs font-semibold uppercase tracking-[0.1em] text-inverse-muted-foreground">
                  {group.label}
                </h4>
                <ul className="mt-4 flex flex-col gap-2.5">
                  {group.links.map((link) => (
                    <li key={link.label}>
                      <Link
                        href={link.href}
                        {...(link.href.startsWith('http')
                          ? { target: '_blank', rel: 'noreferrer' }
                          : {})}
                        className="text-sm text-inverse-muted-foreground transition-colors hover:text-inverse-foreground"
                      >
                        {link.label}
                      </Link>
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </div>
        </div>

        {/* Bottom: copyright + language switcher */}
        <div className="flex items-center justify-between py-6">
          <p className="text-xs text-inverse-muted-foreground">
            {t.footer.copyright.replace('{year}', String(new Date().getFullYear()))}
          </p>
          <div className="flex items-center">
            {locales.map((l, i) => (
              <button
                type="button"
                key={l}
                onClick={() => setLocale(l)}
                aria-pressed={l === locale}
                className={cn(
                  'px-1.5 py-1 text-xs font-medium transition-colors',
                  l === locale
                    ? 'text-inverse-foreground'
                    : 'text-inverse-muted-foreground hover:text-inverse-foreground',
                  i > 0 && 'border-l border-inverse-foreground/15',
                )}
              >
                {localeLabels[l]}
              </button>
            ))}
          </div>
        </div>

        {/* Giant logo */}
        <div className="relative overflow-hidden pb-4">
          <div className="flex items-end gap-6 sm:gap-8">
            <GoosarIcon
              className="size-[clamp(4rem,12vw,10rem)] shrink-0 text-inverse-foreground"
              noSpin
            />
            <span className="font-[family-name:var(--font-serif)] text-[clamp(6rem,22vw,16rem)] font-normal leading-[0.82] tracking-[-0.04em] text-inverse-foreground lowercase">
              goosar
            </span>
          </div>
        </div>
      </div>
    </footer>
  );
}
