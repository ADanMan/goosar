/**
 * @vitest-environment jsdom
 *
 * Проверяет, что деградация REST-транспорта не включает фоновый опрос для
 * ВСЕХ остальных интервалов в приложении. query-core решает, тикать ли
 * интервалу, по видимости документа, а не по фокусу окна, поэтому глобальный
 * `refetchIntervalInBackground: true` снял бы это ограничение сразу для всех
 * существующих поллеров — билдера агентов, онбординга, биллинга, провижининга
 * рантаймов в облаке, — а не только для деградированного фолбэка.
 */
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, renderHook, waitFor } from '@testing-library/react';
import { QueryClientProvider, focusManager, useQuery } from '@tanstack/react-query';
import type { QueryClient } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { createQueryClient } from './query-client';

afterEach(() => {
  cleanup();
  focusManager.setFocused(undefined);
});

function renderPoller(queryClient: QueryClient) {
  const queryFn = vi.fn(async () => 'ok');
  renderHook(
    () =>
      useQuery({
        queryKey: ['degraded-poll'],
        queryFn,
        refetchInterval: 20,
      }),
    {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      ),
    },
  );
  return queryFn;
}

describe('createQueryClient — interval polling and document visibility (#257)', () => {
  it('does not tick any interval while the document is hidden', async () => {
    focusManager.setFocused(false);
    const queryClient = createQueryClient();
    const queryFn = renderPoller(queryClient);

    await waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1));
    await new Promise((resolve) => setTimeout(resolve, 200));
    expect(queryFn).toHaveBeenCalledTimes(1);
    queryClient.clear();
  });

  it('resumes ticking on the next period once the document is visible again', async () => {
    focusManager.setFocused(false);
    const queryClient = createQueryClient();
    const queryFn = renderPoller(queryClient);

    await waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1));
    focusManager.setFocused(true);
    await waitFor(() => expect(queryFn.mock.calls.length).toBeGreaterThan(1), {
      timeout: 2000,
    });
    queryClient.clear();
  });
});
