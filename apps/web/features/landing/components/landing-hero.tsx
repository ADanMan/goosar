'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { Download } from 'lucide-react';
import { useAuthStore } from '@goosar/core/auth';
import { AnimatedSpan, Terminal } from '@goosar/ui/components/ui/terminal';
import { useLocale } from '../i18n';
import { useDashboardCtaHref } from '../utils/use-dashboard-cta';
import { demoLog } from '../demo-log';

const HERO_BUTTON_SOLID =
  'inline-flex items-center justify-center gap-2 rounded-[12px] bg-brand px-5 py-3 text-[14px] font-semibold text-brand-foreground transition-colors hover:opacity-90';
const HERO_BUTTON_GHOST =
  'inline-flex items-center justify-center gap-2 rounded-[12px] border border-white/18 bg-white/8 px-5 py-3 text-[14px] font-semibold text-white backdrop-blur-sm transition-colors hover:bg-white/14';

export function LandingHero() {
  const { t } = useLocale();
  const user = useAuthStore((s) => s.user);
  const ctaHref = useDashboardCtaHref();
  const reducedMotion = usePrefersReducedMotion();

  return (
    <div className="relative min-h-full overflow-hidden bg-rail text-white">
      <LandingBackdrop />

      <main className="relative z-10">
        <section
          id="product"
          className="mx-auto max-w-[1320px] px-4 pb-16 pt-28 sm:px-6 sm:pt-32 lg:px-8 lg:pb-24 lg:pt-36"
        >
          <div className="mx-auto max-w-[1120px] text-center">
            <h1 className="font-heading text-[3.65rem] leading-[0.93] tracking-[-0.038em] sm:text-[4.85rem] lg:text-[6.4rem]">
              {t.hero.headlineLine1}
              <br />
              {t.hero.headlineLine2}
            </h1>

            <p className="mx-auto mt-7 max-w-[820px] text-[15px] leading-7 text-white/72 sm:text-[17px]">
              {t.hero.subheading}
            </p>

            <div className="mt-8 flex flex-wrap items-center justify-center gap-3">
              <Link href={ctaHref} className={HERO_BUTTON_SOLID}>
                {user ? t.header.dashboard : t.hero.cta}
              </Link>
              <Link href="/download" className={HERO_BUTTON_GHOST}>
                <Download className="size-4" aria-hidden />
                {t.hero.downloadDesktop}
              </Link>
            </div>
          </div>

          <div className="mt-10 flex flex-wrap items-center justify-center gap-x-6 gap-y-3">
            <span className="text-[15px] text-white/50">{t.hero.worksWith}</span>
            <span className="text-[15px] font-medium text-white/80">{t.hero.runtimeCount}</span>
          </div>

          <div id="preview" className="mt-10 sm:mt-12 flex justify-center">
            <DemoTerminal lines={t.demoLog.lines} reducedMotion={reducedMotion} />
          </div>
        </section>
      </main>
    </div>
  );
}

function usePrefersReducedMotion(): boolean {
  const [reduced, setReduced] = useState(false);
  useEffect(() => {
    const mql = window.matchMedia('(prefers-reduced-motion: reduce)');
    setReduced(mql.matches);
    const onChange = () => setReduced(mql.matches);
    mql.addEventListener('change', onChange);
    return () => mql.removeEventListener('change', onChange);
  }, []);
  return reduced;
}

function DemoTerminal({
  lines,
  reducedMotion,
}: {
  lines: Record<string, string>;
  reducedMotion: boolean;
}) {
  if (reducedMotion) {
    return (
      <Terminal className="max-h-none max-w-[560px] bg-black/24 text-left text-white" sequence={false}>
        {demoLog.map((line) => (
          <div key={line.key} className="grid text-sm font-normal tracking-tight">
            {lines[line.key]}
          </div>
        ))}
      </Terminal>
    );
  }

  return (
    <Terminal className="max-h-none max-w-[560px] bg-black/24 text-left text-white">
      {demoLog.map((line) => (
        <AnimatedSpan key={line.key}>{lines[line.key]}</AnimatedSpan>
      ))}
    </Terminal>
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
