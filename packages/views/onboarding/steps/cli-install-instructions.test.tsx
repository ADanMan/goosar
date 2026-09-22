import { render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';
import { I18nProvider } from '@goosar/core/i18n/react';
import { configStore } from '@goosar/core/config';
import enCommon from '../../locales/en/common.json';
import enOnboarding from '../../locales/en/onboarding.json';
import { CliInstallInstructions } from './cli-install-instructions';

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

const ligatureClasses = ['[font-variant-ligatures:none]', "[font-feature-settings:'liga'_0]"];

function renderInstructions(config?: { daemonServerUrl?: string; daemonAppUrl?: string }) {
  if (config) {
    configStore.getState().setDaemonConfig(config);
  }
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <CliInstallInstructions />
    </I18nProvider>,
  );
}

describe('CliInstallInstructions', () => {
  beforeEach(() => {
    configStore.getState().setDaemonConfig({
      daemonServerUrl: '',
      daemonAppUrl: '',
    });
  });

  it('disables font ligatures in CLI command code', () => {
    renderInstructions();

    expect(screen.getByText('goosar setup')).toHaveClass(...ligatureClasses);
  });

  it('uses the app-served installer and cloud setup by default', () => {
    const { baseElement } = renderInstructions();

    expect(baseElement).toHaveTextContent(
      'curl -fsSL https://goosar.ru/install.sh | bash -s -- --app-url https://goosar.ru --server-url https://goosar.ru',
    );
    expect(baseElement).not.toHaveTextContent('raw.githubusercontent.com');
    expect(baseElement).toHaveTextContent('goosar setup');
    expect(baseElement).not.toHaveTextContent('goosar setup self-host');
  });

  it('uses self-host daemon URLs from runtime config', () => {
    const { baseElement } = renderInstructions({
      daemonServerUrl: 'https://api.example.com/',
      daemonAppUrl: 'https://app.example.com/',
    });

    expect(baseElement).toHaveTextContent(
      'curl -fsSL https://app.example.com/install.sh | bash -s -- --app-url https://app.example.com --server-url https://api.example.com',
    );
    expect(baseElement).toHaveTextContent(
      'goosar setup self-host --server-url https://api.example.com --app-url https://app.example.com',
    );
  });
});
