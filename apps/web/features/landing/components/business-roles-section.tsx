'use client';

import { useLocale } from '../i18n';

export function BusinessRolesSection() {
  const { t } = useLocale();

  return (
    <section id="business-roles" className="border-t border-border bg-background text-foreground">
      <div className="mx-auto max-w-[1320px] px-4 py-24 sm:px-6 sm:py-32 lg:px-8 lg:py-40">
        <p className="text-2xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">
          {t.businessRoles.label}
        </p>
        <h2 className="mt-4 max-w-[880px] font-[family-name:var(--font-serif)] text-xl leading-[1.05] tracking-[-0.03em] sm:text-2xl">
          {t.businessRoles.title}
        </h2>
        <p className="mt-6 max-w-[640px] text-sm leading-7 text-muted-foreground sm:text-base">
          {t.businessRoles.description}
        </p>

        <div className="mt-16 grid gap-px overflow-hidden rounded-2xl border border-border bg-border sm:grid-cols-2 lg:grid-cols-4">
          {t.businessRoles.roles.map((role) => (
            <div key={role.title} className="bg-card p-8 lg:p-10">
              <h3 className="text-base font-semibold leading-snug text-foreground sm:text-lg">
                {role.title}
              </h3>
              <p className="mt-3 text-sm leading-[1.7] text-muted-foreground">
                {role.description}
              </p>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
