// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enSettings from '../../locales/en/settings.json';

const mockPush = vi.hoisted(() => vi.fn());
const userRef = vi.hoisted(() => ({
  current: { id: 'u1', name: 'Ann', profile_description: '' } as Record<string, unknown> | null,
}));

vi.mock('@goosar/core/auth', () => ({
  useAuthStore: (selector: (s: unknown) => unknown) =>
    selector({ user: userRef.current, setUser: vi.fn() }),
}));

vi.mock('@goosar/core/api', () => ({
  api: { updateMe: vi.fn() },
}));

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock('../../navigation', () => ({
  useNavigation: () => ({ push: mockPush }),
}));

vi.mock('../../common/avatar-upload-control', () => ({
  AvatarUploadControl: () => <div data-testid="avatar-upload" />,
}));

import { AccountTab } from './account-tab';

function renderTab(kerberosSlot?: React.ReactNode, hasKerberos = true) {
  return render(
    <I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings } }}>
      <AccountTab kerberosSlot={kerberosSlot} hasKerberos={hasKerberos} />
    </I18nProvider>,
  );
}

const kerberosHeading = () => screen.queryByText(enSettings.account.section_kerberos);

describe('Settings AccountTab', () => {
  beforeEach(() => {
    cleanup();
    mockPush.mockClear();
  });

  it('navigates to the onboarding replay route without resetting anything', async () => {
    renderTab();
    const button = screen.getByRole('button', {
      name: enSettings.account.replay_onboarding_action,
    });
    await userEvent.click(button);
    expect(mockPush).toHaveBeenCalledWith('/onboarding?replay=1');
  });

  it('omits the Kerberos section when no slot is injected', () => {
    renderTab();
    expect(screen.queryByTestId('kerberos-slot')).not.toBeInTheDocument();
    expect(kerberosHeading()).toBeNull();
  });

  it('omits the Kerberos section outside the perimeter', () => {
    renderTab(<div data-testid="kerberos-slot">ticket</div>, false);
    expect(kerberosHeading()).toBeNull();
  });

  it('renders the injected Kerberos slot when the platform supplies one', () => {
    renderTab(<div data-testid="kerberos-slot">ticket</div>);
    expect(screen.getByTestId('kerberos-slot')).toBeInTheDocument();
  });
});
