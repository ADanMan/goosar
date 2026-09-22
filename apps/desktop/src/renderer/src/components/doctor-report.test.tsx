import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import type { DoctorReport } from '../../../shared/daemon-types';

vi.mock('@goosar/views/i18n', async () => {
  const { settingsI18nMock } = await import('../../../../test/i18n-settings-mock');
  return { ...(await settingsI18nMock()), useUiLocale: () => 'en' };
});

import { DoctorSection } from './doctor-report';

const REPORT: DoctorReport = {
  ok: false,
  checkedAt: 1_700_000_000_000,
  results: [
    {
      id: 'node',
      name: 'Node.js',
      status: 'ok',
      detail: '/usr/local/bin/node',
      message: 'Node нужен.',
      fix: 'brew install node',
    },
    {
      id: 'kinit',
      name: 'Kerberos (kinit)',
      status: 'skipped',
      message: 'Kerberos не используется.',
      fix: '',
    },
    {
      id: 'server',
      name: 'Сервер',
      status: 'missing',
      detail: '',
      message: 'Сервер не ответил: connection refused',
      fix: 'Проверьте адрес стенда.',
    },
    {
      id: 'kit-platform',
      name: 'Кит для этой платформы',
      status: 'missing',
      message: 'Пакетов 12, но ни одного для win-x64',
      fix: 'Соберите пакеты под платформу.',
    },
  ],
};

function installApi(doctor: ReturnType<typeof vi.fn>) {
  Object.defineProperty(window, 'daemonAPI', { configurable: true, value: { doctor } });
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('DoctorSection', () => {
  it("lists the rows with the red ones first and shows the CLI's fix only for them", async () => {
    installApi(vi.fn(async () => REPORT));
    render(<DoctorSection />);

    const rows = await screen.findAllByRole('listitem');
    const names = rows.map((r) => r.textContent ?? '');
    expect(names[0]).toContain('Сервер');
    expect(names[1]).toContain('Кит для этой платформы');
    expect(names[2]).toContain('Node.js');
    expect(names[3]).toContain('Kerberos');

    expect(screen.getByText('Проверьте адрес стенда.')).toBeTruthy();
    expect(screen.getByText('Сервер не ответил: connection refused')).toBeTruthy();
    expect(screen.queryByText('brew install node')).toBeNull();
    expect(screen.getByText('/usr/local/bin/node')).toBeTruthy();
  });

  it('re-runs doctor past the cache when asked', async () => {
    const doctor = vi.fn(async () => REPORT);
    installApi(doctor);
    render(<DoctorSection />);
    await screen.findAllByRole('listitem');

    fireEvent.click(screen.getByRole('button', { name: /снова|re-check|重新|再確認|다시/i }));
    await waitFor(() => expect(doctor).toHaveBeenLastCalledWith({ refresh: true }));
  });

  it('renders a single line, not a broken panel, when the CLI could not run', async () => {
    installApi(vi.fn(async () => null));
    render(<DoctorSection />);
    expect(await screen.findByText(/не удалось запустить|could not run/i)).toBeTruthy();
    expect(screen.queryAllByRole('listitem')).toHaveLength(0);
  });

  it('renders nothing when the preload predates the API', () => {
    Object.defineProperty(window, 'daemonAPI', { configurable: true, value: {} });
    const { container } = render(<DoctorSection />);
    expect(container.textContent).toBe('');
  });
});
