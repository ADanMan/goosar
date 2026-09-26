'use client';

import Image from 'next/image';
import { useLocale } from '../i18n';

const sectionTitleClassName =
  'mt-4 max-w-[880px] font-heading text-[2.6rem] leading-[1.05] tracking-[-0.03em] sm:text-[3.4rem] lg:text-[4.2rem]';

function PointList({ points }: { points: { title: string; description: string }[] }) {
  return (
    <dl className="mt-10 grid gap-8 sm:grid-cols-3">
      {points.map((point) => (
        <div key={point.title}>
          <dt className="text-[15px] font-semibold leading-snug text-[#1c1917] sm:text-[16px]">
            {point.title}
          </dt>
          <dd className="mt-2 text-[14px] leading-[1.7] text-[#1c1917]/60 sm:text-[15px]">
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
    <section id="agent-executor" className="bg-white text-[#1c1917]">
      <div className="mx-auto max-w-[1320px] px-4 py-24 sm:px-6 sm:py-32 lg:px-8 lg:py-40">
        <p className="text-[11px] font-semibold uppercase tracking-[0.16em] text-[#1c1917]/40">
          {section.label}
        </p>
        <h2 className={sectionTitleClassName}>{section.title}</h2>
        <p className="mt-6 max-w-[640px] text-[15px] leading-7 text-[#1c1917]/60 sm:text-[16px]">
          {section.description}
        </p>
        <PointList points={section.points} />
        <div className="mt-16 overflow-hidden rounded-2xl border border-[#1c1917]/8 shadow-sm">
          <Image
            src="/images/app-tasks.png"
            alt={section.imageAlt}
            width={3200}
            height={2000}
            className="w-full"
          />
        </div>
      </div>
    </section>
  );
}

function SelfHostedSection() {
  const { t } = useLocale();
  const section = t.features.selfHosted;

  return (
    <section id="self-hosted" className="bg-[#141210] text-white">
      <div className="mx-auto max-w-[1320px] px-4 py-24 sm:px-6 sm:py-32 lg:px-8 lg:py-40">
        <p className="text-[11px] font-semibold uppercase tracking-[0.16em] text-white/40">
          {section.label}
        </p>
        <h2 className={sectionTitleClassName}>{section.title}</h2>
        <p className="mt-6 max-w-[640px] text-[15px] leading-7 text-white/60 sm:text-[16px]">
          {section.description}
        </p>

        <div className="mt-16 grid gap-px overflow-hidden rounded-2xl border border-white/10 bg-white/10 sm:grid-cols-3">
          {section.points.map((point) => (
            <div key={point.title} className="bg-[#141210] p-8 lg:p-10">
              <h3 className="text-[17px] font-semibold leading-snug text-white sm:text-[18px]">
                {point.title}
              </h3>
              <p className="mt-3 text-[14px] leading-[1.7] text-white/50 sm:text-[15px]">
                {point.description}
              </p>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

function HermesSection() {
  const { t } = useLocale();
  const section = t.features.hermes;

  return (
    <section id="hermes" className="bg-white text-[#1c1917]">
      <div className="mx-auto max-w-[1320px] px-4 py-24 sm:px-6 sm:py-32 lg:px-8 lg:py-40">
        <div className="flex flex-col gap-16 lg:flex-row lg:items-start lg:gap-24">
          <div className="lg:w-[420px] lg:shrink-0">
            <p className="text-[11px] font-semibold uppercase tracking-[0.16em] text-[#1c1917]/40">
              {section.label}
            </p>
            <h2 className="mt-4 font-heading text-[2.6rem] leading-[1.05] tracking-[-0.03em] sm:text-[3.4rem]">
              {section.title}
            </h2>
            <p className="mt-6 text-[15px] leading-7 text-[#1c1917]/60 sm:text-[16px]">
              {section.description}
            </p>
            <dl className="mt-10 space-y-6">
              {section.points.map((point) => (
                <div key={point.title}>
                  <dt className="text-[15px] font-semibold leading-snug text-[#1c1917]">
                    {point.title}
                  </dt>
                  <dd className="mt-1.5 text-[14px] leading-[1.7] text-[#1c1917]/60">
                    {point.description}
                  </dd>
                </div>
              ))}
            </dl>
          </div>

          <div className="flex-1 overflow-hidden rounded-2xl border border-[#1c1917]/8 shadow-sm">
            <Image
              src="/images/app-task.png"
              alt={section.imageAlt}
              width={3200}
              height={2000}
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
    <section id="install" className="bg-[#f8f8f8] text-[#1c1917]">
      <div className="mx-auto max-w-[860px] px-4 py-24 text-center sm:px-6 sm:py-32 lg:py-40">
        <p className="text-[11px] font-semibold uppercase tracking-[0.16em] text-[#1c1917]/40">
          {section.label}
        </p>
        <h2 className={`${sectionTitleClassName} mx-auto`}>{section.title}</h2>
        <p className="mx-auto mt-6 max-w-[520px] text-[15px] leading-7 text-[#1c1917]/60 sm:text-[16px]">
          {section.description}
        </p>

        <pre className="mx-auto mt-10 max-w-[420px] rounded-[12px] bg-[#1c1917] px-6 py-4 text-left font-mono text-[14px] text-white">
          <code>{section.command}</code>
        </pre>
        <p className="mt-4 text-[13px] text-[#1c1917]/50">{section.note}</p>
      </div>
    </section>
  );
}

export function FeaturesSection() {
  return (
    <>
      <AgentExecutorSection />
      <SelfHostedSection />
      <HermesSection />
      <InstallSection />
    </>
  );
}
