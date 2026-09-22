import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import enOnboarding from '../../locales/en/onboarding.json';

vi.mock('../../i18n', () => ({
  useT: () => ({
    t: (selector: (resources: typeof enOnboarding) => string, vars?: Record<string, string>) =>
      selector(enOnboarding).replace(
        /{{(\w+)}}/g,
        (whole: string, key: string) => vars?.[key] ?? whole,
      ),
  }),
}));

import { configStore, EMPTY_DEPLOYMENT_HOSTS } from '@goosar/core/config';
import { WorkToolsCard } from './work-tools-card';

function renderGuide(preset: 'atlassian' | 'outlook' | 'fetch' | 'mcp-gateway') {
  render(<WorkToolsCard preset={preset} />);
  fireEvent.click(screen.getByRole('button', { expanded: false }));
}

function setDeliveryProfile(value: string | undefined) {
  configStore.setState({ deliveryProfile: value } as unknown as Parameters<
    typeof configStore.setState
  >[0]);
}

describe('WorkToolsCard self-install line', () => {
  beforeEach(() => {
    setDeliveryProfile(undefined);
  });

  it('offers the install command to a reader who has no administrator', () => {
    renderGuide('mcp-gateway');
    expect(screen.getByText(/pipx install/)).toBeInTheDocument();
  });

  it('hides it in a perimeter delivery, where the machine is provisioned', () => {
    setDeliveryProfile('perimeter');
    renderGuide('mcp-gateway');
    expect(screen.queryByText(/pipx install/)).toBeNull();
    expect(
      screen.getByText(
        new RegExp(enOnboarding.step_work_tools.presets.mcp_gateway.admin_note.slice(0, 40), 'i'),
      ),
    ).toBeInTheDocument();
  });

  it('says nothing extra for a preset that has no self-install path', () => {
    renderGuide('outlook');
    expect(screen.queryByText(/pipx install/)).toBeNull();
  });

  it('never tells a reader to install a server that ships with the app', () => {
    renderGuide('atlassian');
    expect(screen.queryByText(/pipx install/)).toBeNull();
    renderGuide('fetch');
    expect(screen.queryByText(/pipx install/)).toBeNull();
  });
});

describe('WorkToolsCard deployment addresses (#493)', () => {
  beforeEach(() => {
    setDeliveryProfile(undefined);
    configStore.setState({ deploymentHosts: EMPTY_DEPLOYMENT_HOSTS });
  });

  afterEach(() => {
    configStore.setState({ deploymentHosts: EMPTY_DEPLOYMENT_HOSTS });
  });

  function guideText(): string {
    return document.body.textContent ?? '';
  }

  it("names the deployment's own Jira and Confluence hosts", () => {
    configStore.getState().setDeploymentHosts({
      jiraUrl: 'https://jira.example.test/browse',
      confluenceUrl: 'https://wiki.example.test',
    });
    renderGuide('atlassian');
    const text = guideText();
    expect(text).toContain('jira.example.test');
    expect(text).toContain('wiki.example.test');
    expect(text).not.toContain('jira.example.test/browse');
    expect(text).not.toContain('{{');
  });

  it('falls back to the administrator line when the deployment named none', () => {
    renderGuide('atlassian');
    const text = guideText();
    expect(text).toContain(
      enOnboarding.step_work_tools.presets.atlassian.guide_unknown_hosts.slice(3, 60),
    );
    expect(text).not.toContain('example.test');
    expect(text).not.toContain('{{');
  });

  it('keeps the neutral line when only one of the two hosts is known', () => {
    configStore.getState().setDeploymentHosts({ jiraUrl: 'https://jira.example.test' });
    renderGuide('atlassian');
    expect(guideText()).not.toContain('jira.example.test');
  });
});
