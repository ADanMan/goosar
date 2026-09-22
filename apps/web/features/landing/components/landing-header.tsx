'use client';

import { useState } from 'react';
import Link from 'next/link';
import { Menu, X } from 'lucide-react';
import { GoosarIcon } from '@goosar/ui/components/common/goosar-icon';
import { cn } from '@goosar/ui/lib/utils';
import { useAuthStore } from '@goosar/core/auth';
import { useLocale } from '../i18n';
import { useDashboardCtaHref } from '../utils/use-dashboard-cta';
import { formatStarCount, useGithubStars } from '../utils/github-stars';
import { GitHubMark, githubUrl, headerButtonClassName, type LandingVariant } from './shared';

export function LandingHeader({ variant = 'dark' }: { variant?: LandingVariant }) {
  const { t } = useLocale();
  const user = useAuthStore((s) => s.user);
  const stars = useGithubStars();
  const starsLabel = stars != null ? formatStarCount(stars) : null;
  const [isMenuOpen, setIsMenuOpen] = useState(false);
  const ctaHref = useDashboardCtaHref();
  const ctaLabel = user ? t.header.dashboard : t.header.cta;

  return (
    <header
      className={cn(
        'relative inset-x-0 top-0 z-30',
        variant === 'light' ? 'border-b border-[#1c1917]/8 bg-white' : 'absolute bg-transparent',
      )}
    >
      <div className="mx-auto flex h-[76px] max-w-[1320px] items-center justify-between px-4 sm:px-6 lg:px-8">
        <div className="flex min-w-0 items-center gap-6 lg:gap-8">
          <Link href="/" className="flex shrink-0 items-center gap-3">
            <GoosarIcon
              className={cn(
                'size-10',
                variant === 'dark'
                  ? 'text-white'
                  : variant === 'light'
                    ? 'text-[#1c1917]'
                    : 'text-[#1c1917] dark:text-white',
              )}
              noSpin
            />
            <span
              className={cn(
                'text-[18px] font-semibold tracking-[0.04em] lowercase sm:text-[20px]',
                variant === 'dark'
                  ? 'text-white/92'
                  : variant === 'light'
                    ? 'text-[#1c1917]'
                    : 'text-[#1c1917] dark:text-white/92',
              )}
            >
              goosar
            </span>
          </Link>
        </div>

        <div className="flex shrink-0 items-center gap-2 sm:gap-2.5">
          <button
            type="button"
            aria-label={isMenuOpen ? t.header.closeMenu : t.header.openMenu}
            aria-expanded={isMenuOpen}
            onClick={() => setIsMenuOpen((open) => !open)}
            className={cn(headerButtonClassName('ghost', variant), 'px-3 md:hidden')}
          >
            {isMenuOpen ? (
              <X className="size-4" aria-hidden />
            ) : (
              <Menu className="size-4" aria-hidden />
            )}
          </button>
          <Link
            href={githubUrl}
            target="_blank"
            rel="noreferrer"
            className={cn(headerButtonClassName('ghost', variant), 'hidden lg:inline-flex')}
          >
            <GitHubMark className="size-3.5" />
            {t.header.github}
            {starsLabel ? <GitHubStarsBadge label={starsLabel} /> : null}
          </Link>
          <Link href={ctaHref} className={headerButtonClassName('solid', variant)}>
            {ctaLabel}
          </Link>
        </div>
      </div>

      {isMenuOpen ? (
        <div
          className={cn(
            'absolute left-4 right-4 top-[calc(100%+8px)] z-50 rounded-[14px] border p-2 shadow-[0_18px_60px_rgba(0,0,0,0.18)] backdrop-blur-xl md:hidden',
            variant === 'dark'
              ? 'border-white/14 bg-[#141210]/95 text-white'
              : variant === 'light'
                ? 'border-[#1c1917]/10 bg-white text-[#1c1917]'
                : 'border-[#1c1917]/10 bg-white/95 text-[#1c1917] dark:border-white/14 dark:bg-[#1a1715]/95 dark:text-white',
          )}
        >
          <div>
            <Link
              href={githubUrl}
              target="_blank"
              rel="noreferrer"
              onClick={() => setIsMenuOpen(false)}
              className={mobileNavLinkClassName(variant)}
            >
              <GitHubMark className="size-3.5" />
              {t.header.github}
              {starsLabel ? <GitHubStarsBadge label={starsLabel} /> : null}
            </Link>
          </div>
        </div>
      ) : null}
    </header>
  );
}

function GitHubStarsBadge({ label }: { label: string }) {
  return (
    <span className="inline-flex items-center gap-1.5 tabular-nums">
      <span aria-hidden className="h-3 w-px bg-current opacity-25" />
      {label}
    </span>
  );
}

function mobileNavLinkClassName(variant: LandingVariant) {
  return cn(
    'flex min-h-11 items-center gap-2 rounded-[10px] px-3 text-[14px] font-medium transition-colors',
    variant === 'dark'
      ? 'text-white/76 hover:bg-white/8 hover:text-white'
      : variant === 'light'
        ? 'text-[#1c1917]/68 hover:bg-[#1c1917]/5 hover:text-[#1c1917]'
        : 'text-[#1c1917]/68 hover:bg-[#1c1917]/5 hover:text-[#1c1917] dark:text-white/76 dark:hover:bg-white/8 dark:hover:text-white',
  );
}
