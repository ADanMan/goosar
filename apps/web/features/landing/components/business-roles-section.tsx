'use client';

import { useLocale } from '../i18n';

export function BusinessRolesSection() {
  const { t } = useLocale();

  return (
    <section id="business-roles" className="border-t border-[#1c1917]/8 bg-white text-[#1c1917]">
      <div className="mx-auto max-w-[1320px] px-4 py-24 sm:px-6 sm:py-32 lg:px-8 lg:py-40">
        <p className="text-[11px] font-semibold uppercase tracking-[0.16em] text-[#1c1917]/40">
          {t.businessRoles.label}
        </p>
        <h2 className="mt-4 max-w-[880px] font-[family-name:var(--font-serif)] text-[2.6rem] leading-[1.05] tracking-[-0.03em] sm:text-[3.4rem] lg:text-[4.2rem]">
          {t.businessRoles.title}
        </h2>
        <p className="mt-6 max-w-[640px] text-[15px] leading-7 text-[#1c1917]/60 sm:text-[16px]">
          {t.businessRoles.description}
        </p>

        <div className="mt-16 grid gap-px overflow-hidden rounded-2xl border border-[#1c1917]/8 bg-[#1c1917]/8 sm:grid-cols-2 lg:grid-cols-4">
          {t.businessRoles.roles.map((role) => (
            <div key={role.title} className="bg-white p-8 lg:p-10">
              <h3 className="text-[17px] font-semibold leading-snug text-[#1c1917] sm:text-[18px]">
                {role.title}
              </h3>
              <p className="mt-3 text-[14px] leading-[1.7] text-[#1c1917]/56 sm:text-[15px]">
                {role.description}
              </p>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
