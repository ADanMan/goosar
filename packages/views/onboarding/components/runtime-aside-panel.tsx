'use client';

import { docsUrl } from '@goosar/core/i18n';

import { useT } from '../../i18n';

export function RuntimeAsidePanel() {
  const { t, i18n } = useT('onboarding');
  const installDocHref = docsUrl(i18n.language, '/install-agent-runtime');
  return (
    <div className="flex flex-col gap-6">
      <section>
        <div className="mb-3 text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
          {t(($) => $.runtime_aside.what_eyebrow)}
        </div>
        <p className="text-sm leading-relaxed text-foreground/80">
          {t(($) => $.runtime_aside.what_prefix)}
          <strong className="font-medium text-foreground">
            {t(($) => $.runtime_aside.what_term)}
          </strong>
          {t(($) => $.runtime_aside.what_suffix)}
        </p>
      </section>

      <section>
        <div className="mb-3 text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
          {t(($) => $.runtime_aside.good_eyebrow)}
        </div>
        <div className="flex flex-col gap-4">
          <AsideItem
            glyph="↻"
            title={t(($) => $.runtime_aside.swap_title)}
            body={t(($) => $.runtime_aside.swap_body)}
          />
          <AsideItem
            glyph="∞"
            title={t(($) => $.runtime_aside.add_more_title)}
            body={t(($) => $.runtime_aside.add_more_body)}
          />
        </div>
      </section>

      <a
        href={installDocHref}
        target="_blank"
        rel="noopener noreferrer"
        className="self-start text-sm text-muted-foreground underline underline-offset-4 transition-colors hover:text-foreground"
      >
        {t(($) => $.runtime_aside.learn_more)}
      </a>
    </div>
  );
}

function AsideItem({ glyph, title, body }: { glyph: string; title: string; body: string }) {
  return (
    <div className="grid grid-cols-[22px_1fr] gap-3">
      <div
        aria-hidden
        className="flex h-[20px] w-[20px] items-center justify-center text-sm text-muted-foreground"
      >
        {glyph}
      </div>
      <div className="flex flex-col">
        <div className="text-sm font-medium text-foreground">{title}</div>
        <div className="mt-1 text-xs leading-snug text-muted-foreground">{body}</div>
      </div>
    </div>
  );
}
