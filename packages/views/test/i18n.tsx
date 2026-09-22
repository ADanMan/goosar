import { render, type RenderOptions, type RenderResult } from '@testing-library/react';
import { I18nProvider } from '@goosar/core/i18n/react';
import type { ReactElement, ReactNode } from 'react';
import { RESOURCES } from '../locales';
import type { SupportedLocale } from '@goosar/core/i18n';

type RenderArgs = Omit<RenderOptions, 'wrapper'> & {
  locale?: SupportedLocale;
};

export function renderWithI18n(ui: ReactElement, options: RenderArgs = {}): RenderResult {
  const { locale = 'en', ...rest } = options;
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <I18nProvider locale={locale} resources={RESOURCES}>
        {children}
      </I18nProvider>
    );
  }
  return render(ui, { wrapper: Wrapper, ...rest });
}

export { RESOURCES };
