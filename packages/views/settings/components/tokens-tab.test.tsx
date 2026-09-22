import { beforeEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import { renderWithI18n } from '../../test/i18n';

const listPersonalAccessTokens = vi.fn();

vi.mock('@goosar/core/api', () => ({
  api: {
    listPersonalAccessTokens: () => listPersonalAccessTokens(),
    createPersonalAccessToken: vi.fn(),
    revokePersonalAccessToken: vi.fn(),
  },
}));

import { TokensTab } from './tokens-tab';

const TOKEN = {
  id: 'tok_1',
  name: 'My CLI',
  token_prefix: 'bcs_abcd',
  created_at: '2026-03-03T12:00:00Z',
  last_used_at: '2026-04-09T12:00:00Z',
  expires_at: '2026-06-01T12:00:00Z',
};

describe('TokensTab dates', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listPersonalAccessTokens.mockResolvedValue([TOKEN]);
  });

  it("formats dates in the rendered locale, not the browser's", async () => {
    renderWithI18n(<TokensTab />, { locale: 'ru' });

    const row = await screen.findByText(/Создан/);
    expect(row.textContent).toContain('03.03.2026');
    expect(row.textContent).toContain('09.04.2026');
    expect(row.textContent).toContain('01.06.2026');
    expect(row.textContent).not.toContain('3/3/2026');
  });

  it('still formats English dates for an explicit English choice', async () => {
    renderWithI18n(<TokensTab />, { locale: 'en' });

    await waitFor(() => expect(screen.getByText(/Created/)).toBeTruthy());
    const row = screen.getByText(/Created/);
    expect(row.textContent).toContain('3/3/2026');
    expect(row.textContent).not.toContain('03.03.2026');
  });
});
