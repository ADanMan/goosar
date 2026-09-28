'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { Download } from 'lucide-react';
import { useAuthStore } from '@goosar/core/auth';
import { AnimatedSpan, Terminal } from '@goosar/ui/components/ui/terminal';
import { useLocale } from '../i18n';
import { useDashboardCtaHref } from '../utils/use-dashboard-cta';
import { demoLog } from '../demo-log';
import { heroButtonClassName } from './shared';

const HERO_BUTTON_SOLID = heroButtonClassName('solid');
const HERO_BUTTON_GHOST = heroButtonClassName('ghost');

export function LandingHero() {
  const { t } = useLocale();
  const user = useAuthStore((s) => s.user);
  const ctaHref = useDashboardCtaHref();
  const reducedMotion = usePrefersReducedMotion();

  return (
    <div className="relative min-h-full overflow-hidden bg-inverse text-inverse-foreground">
      <LandingBackdrop />

      <main className="relative z-10">
        <section
          id="product"
          className="mx-auto max-w-[1320px] px-4 pb-16 pt-28 sm:px-6 sm:pt-32 lg:px-8 lg:pb-24 lg:pt-36"
        >
          <div className="mx-auto max-w-[1120px] text-center">
            <h1 className="font-heading text-2xl leading-[0.93] tracking-[-0.038em] sm:text-3xl">
              {t.hero.headlineLine1}
              <br />
              {t.hero.headlineLine2}
            </h1>

            <p className="mx-auto mt-7 max-w-2xl text-sm leading-7 text-inverse-muted-foreground sm:text-base">
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
            <span className="text-sm text-inverse-muted-foreground">{t.hero.worksWith}</span>
            <span className="text-sm font-medium text-inverse-foreground">
              {t.hero.runtimeCount}
            </span>
          </div>

          <div id="preview" className="mt-10 sm:mt-12 flex justify-center">
            {/* The terminal's lines animate in over time; screen readers can't
                reliably pick up content inserted line by line, so the visible
                animation is hidden from assistive tech and a single static
                sr-only transcript with aria-live announces the full log. */}
            <div aria-hidden className="contents">
              <DemoTerminal lines={t.demoLog.lines} reducedMotion={reducedMotion} />
            </div>
            <p className="sr-only" aria-live="polite">
              {demoLog.map((line) => t.demoLog.lines[line.key]).join('. ')}
            </p>
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
      <Terminal
        className="max-h-none max-w-[560px] bg-inverse-foreground/10 text-left text-inverse-foreground"
        sequence={false}
      >
        {demoLog.map((line) => (
          <div key={line.key} className="grid text-sm font-normal tracking-tight">
            {lines[line.key]}
          </div>
        ))}
      </Terminal>
    );
  }

  return (
    <Terminal
      className="max-h-none max-w-[560px] bg-inverse-foreground/10 text-left text-inverse-foreground"
      startOnView={false}
      sequence={false}
    >
      {demoLog.map((line, index) => (
        <AnimatedSpan startOnView={false} delay={index * 350} key={line.key}>
          {lines[line.key]}
        </AnimatedSpan>
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
        className="h-full w-full text-inverse-foreground/[0.06] [mask-image:linear-gradient(to_bottom,black_0%,black_55%,transparent_96%)]"
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
