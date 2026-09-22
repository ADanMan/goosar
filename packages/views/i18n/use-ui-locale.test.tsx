import { describe, expect, it } from 'vitest';
import { renderWithI18n } from '../test/i18n';
import { useUiLocale } from './use-ui-locale';

function Probe() {
  const locale = useUiLocale();
  return (
    <>
      <span data-testid="locale">{locale}</span>
      <span data-testid="date">
        {new Date(Date.UTC(2026, 2, 3)).toLocaleDateString(locale, {
          day: 'numeric',
          month: 'long',
          year: 'numeric',
          timeZone: 'UTC',
        })}
      </span>
    </>
  );
}

describe('useUiLocale', () => {
  it('reports the locale the UI is actually rendered in', () => {
    const ru = renderWithI18n(<Probe />, { locale: 'ru' });
    expect(ru.getByTestId('locale').textContent).toBe('ru');
    expect(ru.getByTestId('date').textContent).toContain('марта');
    ru.unmount();

    const en = renderWithI18n(<Probe />, { locale: 'en' });
    expect(en.getByTestId('locale').textContent).toBe('en');
    expect(en.getByTestId('date').textContent).toContain('March');
    en.unmount();
  });

  it('follows an explicit choice away from the default', () => {
    for (const locale of ['zh-Hans', 'ko', 'ja'] as const) {
      const view = renderWithI18n(<Probe />, { locale });
      expect(view.getByTestId('locale').textContent).toBe(locale);
      view.unmount();
    }
  });

  it('does not fall back to the browser language', () => {
    expect(navigator.language.startsWith('en')).toBe(true);

    const view = renderWithI18n(<Probe />, { locale: 'ru' });
    expect(view.getByTestId('locale').textContent).toBe('ru');
    expect(view.getByTestId('date').textContent).not.toContain('March');
  });
});
