// @vitest-environment jsdom

import { render, cleanup, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { Calendar } from '@goosar/ui/components/ui/calendar';
import { renderWithI18n } from '../test/i18n';
import { DateOnlyPicker } from './date-only-picker';

afterEach(cleanup);

describe('Calendar (shared)', () => {
  function renderWithDropdownMonths(locale: string) {
    return render(
      <Calendar
        mode="single"
        captionLayout="dropdown"
        locale={locale}
        month={new Date(2026, 2, 1)}
        onMonthChange={() => {}}
        startMonth={new Date(2026, 0, 1)}
        endMonth={new Date(2026, 11, 31)}
      />,
    );
  }

  function monthOptionLabels(): (string | null)[] {
    const monthDropdown = screen.getAllByRole('combobox')[0];
    expect(monthDropdown).toBeDefined();
    return within(monthDropdown as HTMLElement)
      .getAllByRole('option')
      .map((option) => option.textContent);
  }

  it('names the months in the given locale', () => {
    renderWithDropdownMonths('ru');
    expect(monthOptionLabels()).toContain('март');
    expect(monthOptionLabels()).not.toContain('Mar');
  });

  it('names the months in English when the UI is English', () => {
    renderWithDropdownMonths('en');
    expect(monthOptionLabels()).toContain('Mar');
  });

  it('localizes the caption, the weekday headers and the first day of the week', () => {
    const { container } = render(
      <Calendar mode="single" locale="ru" month={new Date(2026, 2, 1)} onMonthChange={() => {}} />,
    );
    expect(container.textContent).toContain('март 2026');
    expect(weekdayHeaderCells(container)[0]).toHaveTextContent('пн');
  });
});

function weekdayHeaderCells(root: HTMLElement): HTMLElement[] {
  return within(root).getAllByRole('columnheader', { hidden: true });
}

describe('DateOnlyPicker', () => {
  function renderOpenPicker(locale: 'ru' | 'en') {
    return renderWithI18n(
      <DateOnlyPicker
        value="2026-03-15"
        onChange={() => {}}
        icon={null}
        placeholder="Due date"
        clearLabel="Clear"
        defaultOpen
      />,
      { locale },
    );
  }

  async function weekdayHeaders(): Promise<string> {
    const calendar = await waitFor(() => {
      const node = document.querySelector('[data-slot="calendar"]');
      expect(node).not.toBeNull();
      return node as HTMLElement;
    });
    return weekdayHeaderCells(calendar)
      .map((cell) => cell.textContent)
      .join(' ');
  }

  it('renders the calendar in the UI locale', async () => {
    renderOpenPicker('ru');
    expect(await weekdayHeaders()).toBe('пн вт ср чт пт сб вс');
  });

  it('follows the UI locale when it is English', async () => {
    renderOpenPicker('en');
    expect(await weekdayHeaders()).toBe('Su Mo Tu We Th Fr Sa');
  });
});
