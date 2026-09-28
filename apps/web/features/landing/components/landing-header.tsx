'use client';

import { useState } from 'react';
import Link from 'next/link';
import { Download, Info, Menu, X } from 'lucide-react';
import { GoosarIcon } from '@goosar/ui/components/common/goosar-icon';
import { cn } from '@goosar/ui/lib/utils';
import { useAuthStore } from '@goosar/core/auth';
import { useLocale } from '../i18n';
import { useDashboardCtaHref } from '../utils/use-dashboard-cta';
import { formatStarCount, useGithubStars } from '../utils/github-stars';
import { GitHubMark, githubUrl, headerButtonClassName, type LandingVariant } from './shared';

// Mirrors the header-button split in shared.tsx: `dark` and `hero` both
// float over the always-dark hero/rail band, `light` sits on the plain page.
function isInverseVariant(variant: LandingVariant) {
  return variant !== 'light';
}

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
        variant === 'light' ? 'border-b border-border bg-background' : 'absolute bg-transparent',
      )}
    >
      <div className="mx-auto flex h-[76px] max-w-[1320px] items-center justify-between px-4 sm:px-6 lg:px-8">
        <div className="flex min-w-0 items-center gap-6 lg:gap-8">
          <Link href="/" className="flex shrink-0 items-center gap-3">
            <GoosarIcon
              className={cn(
                'size-10',
                isInverseVariant(variant) ? 'text-inverse-foreground' : 'text-foreground',
              )}
              noSpin
            />
            <span
              className={cn(
                'text-base font-semibold tracking-[0.04em] lowercase sm:text-lg',
                isInverseVariant(variant) ? 'text-inverse-foreground' : 'text-foreground',
              )}
            >
              goosar
            </span>
          </Link>

          <nav aria-label={t.header.navigation} className="hidden items-center gap-6 md:flex">
            <Link href="/download" className={navLinkClassName(variant)}>
              {t.header.download}
            </Link>
            <Link href="/about" className={navLinkClassName(variant)}>
              {t.header.about}
            </Link>
          </nav>
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
            'absolute left-4 right-4 top-[calc(100%+8px)] z-50 rounded-xl border p-2 shadow-md backdrop-blur-xl md:hidden',
            isInverseVariant(variant)
              ? 'border-inverse-foreground/15 bg-inverse/95 text-inverse-foreground'
              : 'border-border bg-card/95 text-foreground',
          )}
        >
          <div>
            <Link
              href="/download"
              onClick={() => setIsMenuOpen(false)}
              className={mobileNavLinkClassName(variant)}
            >
              <Download className="size-3.5" aria-hidden />
              {t.header.download}
            </Link>
            <Link
              href="/about"
              onClick={() => setIsMenuOpen(false)}
              className={mobileNavLinkClassName(variant)}
            >
              <Info className="size-3.5" aria-hidden />
              {t.header.about}
            </Link>
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

function navLinkClassName(variant: LandingVariant) {
  return cn(
    'text-sm font-medium transition-colors',
    isInverseVariant(variant)
      ? 'text-inverse-muted-foreground hover:text-inverse-foreground'
      : 'text-muted-foreground hover:text-foreground',
  );
}

function mobileNavLinkClassName(variant: LandingVariant) {
  return cn(
    'flex min-h-11 items-center gap-2 rounded-lg px-3 text-sm font-medium transition-colors',
    isInverseVariant(variant)
      ? 'text-inverse-muted-foreground hover:bg-inverse-foreground/10 hover:text-inverse-foreground'
      : 'text-muted-foreground hover:bg-muted hover:text-foreground',
  );
}
