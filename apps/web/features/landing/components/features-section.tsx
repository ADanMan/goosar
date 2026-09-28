'use client';

import Image from 'next/image';
import { GitBranch, Server, ShieldCheck } from 'lucide-react';
import { useLocale } from '../i18n';

const sectionTitleClassName =
  'mt-4 max-w-[880px] font-heading text-xl leading-[1.05] tracking-[-0.03em] sm:text-2xl';

// One skimmable image width across both screenshot blocks: the container
// caps at 1320px with page padding, so the rendered image never exceeds
// ~1200px even on wide viewports.
const SCREENSHOT_SIZES = '(min-width: 1320px) 1200px, 100vw';

function PointList({ points }: { points: { title: string; description: string }[] }) {
  return (
    <dl className="mt-10 grid gap-8 sm:grid-cols-3">
      {points.map((point) => (
        <div key={point.title}>
          <dt className="text-sm font-semibold leading-snug text-foreground sm:text-base">
            {point.title}
          </dt>
          <dd className="mt-2 text-sm leading-[1.7] text-muted-foreground">
            {point.description}
          </dd>
        </div>
      ))}
    </dl>
  );
}

function AgentExecutorSection() {
  const { t } = useLocale();
  const section = t.features.agentExecutor;

  return (
    <section id="agent-executor" className="bg-background text-foreground">
      <div className="mx-auto max-w-[1320px] px-4 py-24 sm:px-6 sm:py-32 lg:px-8 lg:py-40">
        <p className="text-2xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">
          {section.label}
        </p>
        <h2 className={sectionTitleClassName}>{section.title}</h2>
        <p className="mt-6 max-w-[640px] text-sm leading-7 text-muted-foreground sm:text-base">
          {section.description}
        </p>
        <PointList points={section.points} />
        <div className="mt-16 overflow-hidden rounded-2xl border border-border shadow-sm">
          <Image
            src="/images/app-tasks.png"
            alt={section.imageAlt}
            width={3200}
            height={2000}
            sizes={SCREENSHOT_SIZES}
            className="w-full"
          />
        </div>
      </div>
    </section>
  );
}

const SELF_HOSTED_ICONS = [Server, ShieldCheck, GitBranch];

function SelfHostedSection() {
  const { t } = useLocale();
  const section = t.features.selfHosted;

  return (
    <section id="self-hosted" className="bg-inverse text-inverse-foreground">
      <div className="mx-auto max-w-[1320px] px-4 py-24 sm:px-6 sm:py-32 lg:px-8 lg:py-40">
        <p className="text-2xs font-semibold uppercase tracking-[0.16em] text-inverse-muted-foreground">
          {section.label}
        </p>
        <h2 className={sectionTitleClassName}>{section.title}</h2>
        <p className="mt-6 max-w-[640px] text-sm leading-7 text-inverse-muted-foreground sm:text-base">
          {section.description}
        </p>

        <div className="mt-16 grid gap-px overflow-hidden rounded-2xl border border-inverse-foreground/15 bg-inverse-foreground/15 sm:grid-cols-3">
          {section.points.map((point, i) => {
            const Icon = SELF_HOSTED_ICONS[i % SELF_HOSTED_ICONS.length]!;
            return (
              <div key={point.title} className="bg-inverse p-8 lg:p-10">
                <Icon className="size-5 text-brand" aria-hidden />
                <h3 className="mt-4 text-base font-semibold leading-snug text-inverse-foreground sm:text-lg">
                  {point.title}
                </h3>
                <p className="mt-3 text-sm leading-[1.7] text-inverse-muted-foreground">
                  {point.description}
                </p>
              </div>
            );
          })}
        </div>
      </div>
    </section>
  );
}

function HermesSection() {
  const { t } = useLocale();
  const section = t.features.hermes;

  return (
    <section id="hermes" className="bg-background text-foreground">
      <div className="mx-auto max-w-[1320px] px-4 py-24 sm:px-6 sm:py-32 lg:px-8 lg:py-40">
        <div className="flex flex-col gap-16 lg:flex-row lg:items-start lg:gap-24">
          <div className="lg:w-[420px] lg:shrink-0">
            <p className="text-2xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">
              {section.label}
            </p>
            <h2 className="mt-4 font-heading text-xl leading-[1.05] tracking-[-0.03em] sm:text-2xl">
              {section.title}
            </h2>
            <p className="mt-6 text-sm leading-7 text-muted-foreground sm:text-base">
              {section.description}
            </p>
            <dl className="mt-10 space-y-6">
              {section.points.map((point) => (
                <div key={point.title}>
                  <dt className="text-sm font-semibold leading-snug text-foreground">
                    {point.title}
                  </dt>
                  <dd className="mt-1.5 text-sm leading-[1.7] text-muted-foreground">
                    {point.description}
                  </dd>
                </div>
              ))}
            </dl>
          </div>

          <div className="flex-1 overflow-hidden rounded-2xl border border-border shadow-sm">
            <Image
              src="/images/app-task.png"
              alt={section.imageAlt}
              width={3200}
              height={2000}
              sizes={SCREENSHOT_SIZES}
              className="w-full"
            />
          </div>
        </div>
      </div>
    </section>
  );
}

function InstallSection() {
  const { t } = useLocale();
  const section = t.features.install;

  return (
    <section id="install" className="bg-muted text-foreground">
      <div className="mx-auto max-w-[860px] px-4 py-24 text-center sm:px-6 sm:py-32 lg:py-40">
        <p className="text-2xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">
          {section.label}
        </p>
        <h2 className={`${sectionTitleClassName} mx-auto`}>{section.title}</h2>
        <p className="mx-auto mt-6 max-w-[520px] text-sm leading-7 text-muted-foreground sm:text-base">
          {section.description}
        </p>

        <pre className="mx-auto mt-10 max-w-[420px] rounded-xl bg-inverse px-6 py-4 text-left font-mono text-sm text-inverse-foreground">
          <code>{section.command}</code>
        </pre>
        <p className="mt-4 text-xs text-muted-foreground">{section.note}</p>
      </div>
    </section>
  );
}

export function FeaturesSection() {
  return (
    <div id="features">
      <AgentExecutorSection />
      <SelfHostedSection />
      <HermesSection />
      <InstallSection />
    </div>
  );
}
