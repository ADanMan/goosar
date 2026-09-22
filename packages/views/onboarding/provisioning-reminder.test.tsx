// @vitest-environment jsdom

import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../locales/en/common.json';
import enOnboarding from '../locales/en/onboarding.json';
import { ProvisioningReminder } from './provisioning-reminder';
import type { ProvisioningStatus } from './provisioning-status';

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

function status(over: Partial<ProvisioningStatus> = {}): ProvisioningStatus {
  const zero = { installed: 0, total: 0, failed: 0 };
  return {
    configured: true,
    state: 'ok',
    byType: { skill: zero, 'mcp-server': zero, runtime: zero },
    summary: { installed: 12, total: 12, failed: 0 },
    packages: [],
    ...over,
  };
}

function renderReminder(value: ProvisioningStatus | null, onRetry = vi.fn()) {
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ProvisioningReminder status={value} onRetry={onRetry} />
    </I18nProvider>,
  );
  return { onRetry };
}

describe('ProvisioningReminder', () => {
  it('says nothing once everything is delivered', () => {
    renderReminder(status());
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('says nothing on a deployment that serves no packages', () => {
    renderReminder(status({ configured: false, state: 'idle' }));
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('says nothing before the bridge has answered', () => {
    renderReminder(null);
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('breaks the silence for the returning member whose packages never came', async () => {
    const { onRetry } = renderReminder(
      status({ state: 'idle', summary: { installed: 0, total: 12, failed: 0 } }),
    );

    expect(screen.getByText(/packages that have not reached this computer/i)).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /deliver now/i }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it('reports progress while a sync is running, and offers no retry mid-flight', () => {
    renderReminder(
      status({
        state: 'syncing',
        summary: { installed: 3, total: 12, failed: 0 },
      }),
    );
    expect(screen.getByText(/3 of 12/i)).toBeInTheDocument();
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('reports a failure with a way to try again', () => {
    renderReminder(status({ state: 'fail', reasonCode: 'network_error' }));
    expect(screen.getByText(/could not be delivered/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /deliver now/i })).toBeInTheDocument();
  });

  it('does not call a partial delivery finished just because the pass ended', () => {
    renderReminder(status({ state: 'ok', summary: { installed: 9, total: 12, failed: 3 } }));
    expect(screen.getByRole('status')).toBeInTheDocument();
  });
});

it('stays quiet while a sync has not yet counted anything', () => {
  renderReminder(status({ state: 'syncing', summary: { installed: 0, total: 0, failed: 0 } }));
  expect(screen.queryByText('Preparing your workspace')).toBeNull();
});

it('still speaks up when that sync has already failed a package', () => {
  renderReminder(status({ state: 'syncing', summary: { installed: 0, total: 0, failed: 1 } }));
  expect(screen.getByText('Preparing your workspace')).toBeInTheDocument();
});

it('names the covered platforms when none of them is this machine', () => {
  renderReminder(
    status({
      state: 'ok',
      summary: { installed: 0, total: 0, failed: 0 },
      reasonCode: 'provisioning_platform_uncovered',
      platform: 'win-x64',
      platformsAvailable: ['*', 'darwin-arm64', 'linux-x64'],
      totalBeforePlatformFilter: 12,
    }),
  );
  const card = screen.getByRole('status');
  expect(card.textContent).toContain('darwin-arm64, linux-x64');
  expect(card.textContent).toContain('win-x64');
  expect(screen.queryByRole('button')).toBeNull();
});

it('lists each failed package with its reason', () => {
  renderReminder(
    status({
      state: 'fail',
      summary: { installed: 1, total: 3, failed: 2 },
      reasonCode: 'provisioning_package_failed',
      packages: [
        { name: 'office-docx', type: 'skill', version: '1.0.0', state: 'ok' },
        {
          name: 'mcp-atlassian',
          type: 'mcp-server',
          version: '0.23.1',
          state: 'fail',
          reasonCode: 'provisioning_sha256_mismatch',
        },
        {
          name: 'ews-mcp',
          type: 'mcp-server',
          version: '0.1.1',
          state: 'fail',
          reasonCode: 'provisioning_download_failed',
        },
      ],
    }),
  );
  const text = screen.getByRole('status').textContent ?? '';
  expect(text).toContain('mcp-atlassian: checksum did not match');
  expect(text).toContain('ews-mcp: could not be downloaded');
  expect(text).not.toContain('office-docx:');
});
