// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enRuntimes from '../../locales/en/runtimes.json';
import { UpdateSection } from './update-section';

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };

vi.mock('@goosar/core/api', () => ({
  api: {
    initiateUpdate: vi.fn(),
    getUpdateResult: vi.fn(),
  },
}));

const configState = { deliveryProfile: '' };
vi.mock('@goosar/core/config', () => ({
  useConfigStore: (selector: (s: typeof configState) => unknown) => selector(configState),
  isPerimeterDeliveryProfile: (profile: string | undefined) => profile === 'perimeter',
}));

function renderSection(props: {
  runtimeId: string | null;
  launchedBy?: string | null;
  currentVersion?: string;
}) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <UpdateSection
        runtimeId={props.runtimeId}
        currentVersion={props.currentVersion ?? 'v0.4.0'}
        isOnline
        launchedBy={props.launchedBy}
      />
    </I18nProvider>,
  );
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  configState.deliveryProfile = '';
});

describe('UpdateSection read-only status', () => {
  it('shows Latest without a redundant read-only label or update action', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ tag_name: 'v0.4.0' }),
      }),
    );

    renderSection({ runtimeId: null });

    expect(await screen.findByText('Latest')).toBeInTheDocument();
    expect(screen.queryByText('Read-only')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Update' })).not.toBeInTheDocument();
  });

  it('shows the Desktop manager without exposing an update action', () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ tag_name: 'v0.4.0' }),
      }),
    );

    renderSection({ runtimeId: null, launchedBy: 'desktop' });

    expect(screen.getByText('Managed by Desktop')).toBeInTheDocument();
    expect(screen.queryByText('Read-only')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Update' })).not.toBeInTheDocument();
  });

  it('shows an available version without an action for a read-only viewer', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ tag_name: 'v0.4.0' }),
      }),
    );

    renderSection({ runtimeId: null, currentVersion: 'v0.3.17' });

    expect(await screen.findByText('available')).toBeInTheDocument();
    expect(screen.getByText('Read-only')).toBeInTheDocument();
    expect(
      screen.getByTitle('Only runtime owners and workspace admins can update the CLI.'),
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Update' })).not.toBeInTheDocument();
  });
});

describe('UpdateSection on the perimeter delivery profile', () => {
  it('shows the operator-managed label and never probes GitHub', async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    configState.deliveryProfile = 'perimeter';

    renderSection({ runtimeId: 'rt-1', currentVersion: 'v0.3.17' });

    expect(screen.getByText('Managed by your operator')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Update' })).not.toBeInTheDocument();
    expect(screen.queryByText('available')).not.toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('keeps the Desktop-managed label when both apply', () => {
    vi.stubGlobal('fetch', vi.fn());
    configState.deliveryProfile = 'perimeter';

    renderSection({ runtimeId: null, launchedBy: 'desktop' });

    expect(screen.getByText('Managed by Desktop')).toBeInTheDocument();
    expect(screen.queryByText('Managed by your operator')).not.toBeInTheDocument();
  });
});
