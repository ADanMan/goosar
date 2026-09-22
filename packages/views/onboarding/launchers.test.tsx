// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../locales/en/common.json';
import enOnboarding from '../locales/en/onboarding.json';

const invalidateRef = vi.hoisted(() => vi.fn());
const workToolsPropsRef = vi.hoisted(() => ({
  current: null as Record<string, unknown> | null,
}));
const runtimePropsRef = vi.hoisted(() => ({
  current: null as Record<string, unknown> | null,
}));

vi.mock('@tanstack/react-query', () => ({
  useQueryClient: () => ({ invalidateQueries: invalidateRef }),
}));

vi.mock('./steps/step-work-tools', () => ({
  StepWorkTools: (props: Record<string, unknown>) => {
    workToolsPropsRef.current = props;
    return (
      <button data-testid="work-tools-step" onClick={() => (props.onFinish as () => void)()}>
        finish
      </button>
    );
  },
}));

vi.mock('./steps/step-runtime-connect', () => ({
  StepRuntimeConnect: (props: Record<string, unknown>) => {
    runtimePropsRef.current = props;
    return (
      <button
        data-testid="runtime-step"
        onClick={() => (props.onNext as (r: unknown) => void)({ id: 'rt-1' })}
      >
        pick
      </button>
    );
  },
}));

import { RuntimeConnectButton, WorkToolsSetupButton } from './launchers';

function wrap(node: React.ReactNode) {
  return render(
    <I18nProvider locale="en" resources={{ en: { common: enCommon, onboarding: enOnboarding } }}>
      {node}
    </I18nProvider>,
  );
}

describe('onboarding step launchers', () => {
  beforeEach(() => {
    cleanup();
    invalidateRef.mockClear();
    workToolsPropsRef.current = null;
    runtimePropsRef.current = null;
  });

  it('opens the work-tools step without rebinding the Helper to a runtime', async () => {
    wrap(<WorkToolsSetupButton wsId="ws-1" />);

    expect(screen.queryByTestId('work-tools-step')).toBeNull();
    await userEvent.click(
      screen.getByRole('button', {
        name: enOnboarding.launchers.configure_work_tools,
      }),
    );

    expect(screen.getByTestId('work-tools-step')).toBeInTheDocument();
    expect(workToolsPropsRef.current).toMatchObject({
      wsId: 'ws-1',
      runtime: null,
    });
  });

  it("passes the platform's work-tools extras through", async () => {
    wrap(
      <WorkToolsSetupButton
        wsId="ws-1"
        extras={{ perimeterCaMissing: true, installedMcpNames: ['ews-mcp'] }}
      />,
    );
    await userEvent.click(
      screen.getByRole('button', {
        name: enOnboarding.launchers.configure_work_tools,
      }),
    );

    expect(workToolsPropsRef.current).toMatchObject({
      perimeterCaMissing: true,
      installedMcpNames: ['ews-mcp'],
    });
  });

  it('does not hand the step an empty installed list', async () => {
    wrap(<WorkToolsSetupButton wsId="ws-1" extras={{ installedMcpNames: [] }} />);
    await userEvent.click(
      screen.getByRole('button', {
        name: enOnboarding.launchers.configure_work_tools,
      }),
    );

    expect(workToolsPropsRef.current?.installedMcpNames).toBeUndefined();
  });

  it('closes the work-tools overlay when the step finishes', async () => {
    wrap(<WorkToolsSetupButton wsId="ws-1" />);
    await userEvent.click(
      screen.getByRole('button', {
        name: enOnboarding.launchers.configure_work_tools,
      }),
    );
    await userEvent.click(screen.getByTestId('work-tools-step'));

    expect(screen.queryByTestId('work-tools-step')).toBeNull();
  });

  it('invalidates the runtime and agent lists after a machine is connected', async () => {
    wrap(<RuntimeConnectButton wsId="ws-1" />);
    await userEvent.click(
      screen.getByRole('button', {
        name: enOnboarding.launchers.connect_machine,
      }),
    );
    expect(runtimePropsRef.current).toMatchObject({ wsId: 'ws-1' });

    await userEvent.click(screen.getByTestId('runtime-step'));

    expect(screen.queryByTestId('runtime-step')).toBeNull();
    const keys = invalidateRef.mock.calls.map((c) => JSON.stringify(c[0]?.queryKey));
    expect(keys).toContain(JSON.stringify(['runtimes', 'ws-1']));
    expect(keys).toContain(JSON.stringify(['workspaces', 'ws-1', 'agents']));
  });
});
