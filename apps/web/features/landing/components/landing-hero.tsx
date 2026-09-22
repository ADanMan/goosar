'use client';

import Image from 'next/image';
import Link from 'next/link';
import { Download } from 'lucide-react';
import { useAuthStore } from '@goosar/core/auth';
import { useLocale } from '../i18n';
import { useDashboardCtaHref } from '../utils/use-dashboard-cta';
import { heroButtonClassName } from './shared';

export function LandingHero() {
  const { t } = useLocale();
  const user = useAuthStore((s) => s.user);
  const ctaHref = useDashboardCtaHref();

  return (
    <div className="relative min-h-full overflow-hidden bg-[linear-gradient(180deg,#ffffff_0%,#f2f3f5_100%)] text-[#1c1917] dark:bg-[linear-gradient(180deg,#0f1115_0%,#171a1f_100%)] dark:text-white">
      <LandingBackdrop />

      <main className="relative z-10">
        <section
          id="product"
          className="mx-auto max-w-[1320px] px-4 pb-16 pt-28 sm:px-6 sm:pt-32 lg:px-8 lg:pb-24 lg:pt-36"
        >
          <div className="mx-auto max-w-[1120px] text-center">
            <h1 className="font-[family-name:var(--font-serif)] text-[3.65rem] leading-[0.93] tracking-[-0.038em] sm:text-[4.85rem] lg:text-[6.4rem]">
              {t.hero.headlineLine1}
              <br />
              {t.hero.headlineLine2}
            </h1>

            <p className="mx-auto mt-7 max-w-[820px] text-[15px] leading-7 text-[#1c1917]/64 sm:text-[17px] dark:text-white/80">
              {t.hero.subheading}
            </p>

            <div className="mt-8 flex flex-wrap items-center justify-center gap-3">
              <Link href={ctaHref} className={heroButtonClassName('solid', 'hero')}>
                {user ? t.header.dashboard : t.hero.cta}
              </Link>
              <Link href="/download" className={heroButtonClassName('ghost', 'hero')}>
                <Download className="size-4" aria-hidden />
                {t.hero.downloadDesktop}
              </Link>
            </div>
          </div>

          <div className="mt-10 flex flex-wrap items-center justify-center gap-x-6 gap-y-3">
            <span className="text-[15px] text-[#1c1917]/45 dark:text-white/50">
              {t.hero.worksWith}
            </span>
            <span className="text-[15px] font-medium text-[#1c1917]/78 dark:text-white/80">
              {t.hero.runtimeCount}
            </span>
          </div>

          <div id="preview" className="mt-10 sm:mt-12">
            <ProductImage alt={t.hero.imageAlt} />
          </div>
        </section>
      </main>
    </div>
  );
}

const HEX_SIDE = 44;
const HEX_W = Math.sqrt(3) * HEX_SIDE; 
const HEX_H = 3 * HEX_SIDE; 

const HEX_PATH = [
  `M0 ${HEX_SIDE / 2} L${HEX_W / 2} 0 L${HEX_W} ${HEX_SIDE / 2}`,
  `M0 ${HEX_SIDE / 2} V${1.5 * HEX_SIDE}`,
  `M${HEX_W} ${HEX_SIDE / 2} V${1.5 * HEX_SIDE}`,
  `M0 ${1.5 * HEX_SIDE} L${HEX_W / 2} ${2 * HEX_SIDE} L${HEX_W} ${1.5 * HEX_SIDE}`,
  `M${HEX_W / 2} ${2 * HEX_SIDE} V${HEX_H}`,
].join(' ');

function LandingBackdrop() {
  return (
    <div className="pointer-events-none absolute inset-0" aria-hidden>
      <svg
        className="h-full w-full text-[#1c1917]/[0.05] dark:text-white/[0.06] [mask-image:linear-gradient(to_bottom,black_0%,black_55%,transparent_96%)]"
        xmlns="http://www.w3.org/2000/svg"
      >
        <defs>
          <pattern
            id="landing-honeycomb"
            width={HEX_W}
            height={HEX_H}
            patternUnits="userSpaceOnUse"
          >
            <path d={HEX_PATH} fill="none" stroke="currentColor" strokeWidth="1" />
          </pattern>
        </defs>
        <rect width="100%" height="100%" fill="url(#landing-honeycomb)" />
      </svg>
    </div>
  );
}

function ProductImage({ alt }: { alt: string }) {
  return (
    <div className="relative">
      {/* Warm brand-orange radial glow behind the product screenshot. */}
      <div
        aria-hidden
        className="pointer-events-none absolute left-1/2 top-[-14%] h-[72%] w-[112%] -translate-x-1/2 rounded-[100%] bg-[radial-gradient(50%_50%_at_50%_50%,rgba(249,115,22,0.28),rgba(249,115,22,0))] blur-3xl dark:bg-[radial-gradient(50%_50%_at_50%_50%,rgba(249,115,22,0.16),rgba(249,115,22,0))]"
      />
      <div className="relative overflow-hidden border border-[#1c1917]/10 shadow-[0_28px_90px_rgba(28,25,23,0.16)] dark:border-white/14 dark:shadow-[0_28px_90px_rgba(0,0,0,0.5)]">
        <Image
          src="/images/landing-hero.png"
          alt={alt}
          width={3532}
          height={2382}
          priority
          className="block h-auto w-full"
          sizes="(max-width: 1320px) 100vw, 1320px"
          quality={85}
        />
      </div>
    </div>
  );
}
