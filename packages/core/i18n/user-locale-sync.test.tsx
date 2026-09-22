// @vitest-environment jsdom

import { cleanup, render, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { I18nProvider } from './provider';
import { LocaleAdapterProvider } from './adapter-context';
import { createBrowserCookieLocaleAdapter } from './browser-cookie-adapter';
import { pickLocale } from './pick-locale';
import type { LocaleAdapter, SupportedLocale } from './types';

const userRef = vi.hoisted(() => ({
  current: null as { language: string | null } | null,
}));
const mockReload = vi.hoisted(() => vi.fn());

vi.mock('../auth', () => {
  type State = { user: typeof userRef.current };
  const state = (): State => ({ user: userRef.current });
  const useAuthStore = Object.assign(
    (sel?: (s: State) => unknown) => (sel ? sel(state()) : state()),
    { getState: state },
  );
  return { useAuthStore };
});

import { UserLocaleSync } from './user-locale-sync';

function clearCookies() {
  document.cookie
    .split(';')
    .map((c) => c.trim().split('=')[0])
    .filter(Boolean)
    .forEach((name) => {
      document.cookie = `${name}=; path=/; expires=Thu, 01 Jan 1970 00:00:00 GMT`;
    });
}

function renderSync(locale: SupportedLocale, adapter: LocaleAdapter) {
  return render(
    <I18nProvider locale={locale} resources={{ [locale]: {} }}>
      <LocaleAdapterProvider adapter={adapter}>
        <UserLocaleSync />
      </LocaleAdapterProvider>
    </I18nProvider>,
  );
}

describe('UserLocaleSync', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    clearCookies();
    userRef.current = null;
    Object.defineProperty(window, 'location', {
      writable: true,
      configurable: true,
      value: { reload: mockReload, protocol: 'http:' },
    });
  });

  afterEach(() => {
    cleanup();
    clearCookies();
  });

  it('turns a server-stored English choice into a local choice that outlives the Russian default', async () => {
    const adapter = createBrowserCookieLocaleAdapter();
    expect(pickLocale(adapter)).toBe('ru');

    userRef.current = { language: 'en' };
    renderSync('ru', adapter);

    await waitFor(() => expect(mockReload).toHaveBeenCalledTimes(1));
    expect(adapter.getUserChoice()).toBe('en');
    expect(pickLocale(adapter)).toBe('en');
    expect(pickLocale(createBrowserCookieLocaleAdapter())).toBe('en');
  });

  it('records a choice that already matches the rendered locale', async () => {
    const adapter = createBrowserCookieLocaleAdapter();
    expect(adapter.getUserChoice()).toBe(null);

    userRef.current = { language: 'ru' };
    renderSync('ru', adapter);

    await waitFor(() => expect(adapter.getUserChoice()).toBe('ru'));
    expect(mockReload).not.toHaveBeenCalled();
    expect(pickLocale(createBrowserCookieLocaleAdapter())).toBe('ru');
  });

  it('leaves an already-applied English choice alone', async () => {
    const adapter = createBrowserCookieLocaleAdapter();
    adapter.persist('en');
    userRef.current = { language: 'en' };

    renderSync('en', adapter);

    await Promise.resolve();
    expect(mockReload).not.toHaveBeenCalled();
    expect(pickLocale(adapter)).toBe('en');
  });

  it('does not write a choice for a user who never picked a language', async () => {
    const adapter = createBrowserCookieLocaleAdapter();
    userRef.current = { language: null };

    renderSync('ru', adapter);

    await Promise.resolve();
    expect(mockReload).not.toHaveBeenCalled();
    expect(adapter.getUserChoice()).toBe(null);
    expect(pickLocale(adapter)).toBe('ru');
  });

  it('ignores a stored language that is not a supported locale', async () => {
    const adapter = createBrowserCookieLocaleAdapter();
    userRef.current = { language: 'fr' };

    renderSync('ru', adapter);

    await Promise.resolve();
    expect(mockReload).not.toHaveBeenCalled();
    expect(adapter.getUserChoice()).toBe(null);
    expect(pickLocale(adapter)).toBe('ru');
  });

  it('restores every other explicitly stored locale, not just English', async () => {
    for (const language of ['zh-Hans', 'ko', 'ja'] as SupportedLocale[]) {
      clearCookies();
      mockReload.mockClear();
      const adapter = createBrowserCookieLocaleAdapter();
      userRef.current = { language };

      renderSync('ru', adapter);

      await waitFor(() => expect(mockReload).toHaveBeenCalledTimes(1));
      expect(pickLocale(adapter)).toBe(language);
      cleanup();
    }
  });
});
