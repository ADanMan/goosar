import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render, screen } from '@testing-library/react';

import enSettings from '@goosar/views/locales/en/settings.json';
import type { DaemonStatus } from '../../../shared/daemon-types';

vi.mock('@goosar/views/i18n', () => ({
  useT: () => ({
    t: (selector: (resources: typeof enSettings) => string, vars?: Record<string, string>) => {
      const template = selector(enSettings);
      return vars ? template.replace(/{{(\w+)}}/g, (_, key: string) => vars[key] ?? '') : template;
    },
  }),
}));

const mocks = vi.hoisted(() => ({
  toastSuccess: vi.fn(),
}));

vi.mock('sonner', () => ({
  toast: { success: mocks.toastSuccess, error: vi.fn() },
}));

const logLineCallback = vi.hoisted(() => ({ current: null as ((line: string) => void) | null }));

Object.assign(window, {
  daemonAPI: {
    startLogStream: vi.fn(),
    stopLogStream: vi.fn(),
    onLogLine: (cb: (line: string) => void) => {
      logLineCallback.current = cb;
      return () => {
        logLineCallback.current = null;
      };
    },
  },
});

import { DaemonPanel } from './daemon-panel';

const STATUS: DaemonStatus = { state: 'running', uptime: '5m' };

function pushLine(raw: string) {
  act(() => {
    logLineCallback.current?.(raw);
  });
}

describe('DaemonPanel log clearing (§3 L94)', () => {
  beforeEach(() => {
    mocks.toastSuccess.mockClear();
  });

  it('clears the buffer through an undo toast instead of a confirm dialog, and restores it on undo', () => {
    render(
      <DaemonPanel open status={STATUS} runtimeCount={1} onOpenChange={() => {}} />,
    );

    pushLine('12:00:00.000 INFO daemon ready');
    expect(screen.getByText('daemon ready')).toBeTruthy();

    act(() => {
      screen.getByRole('button', { name: 'Clear' }).click();
    });

    // The buffer is cleared immediately — no confirm dialog blocks it.
    expect(screen.queryByText('daemon ready')).toBeNull();
    expect(screen.queryByRole('alertdialog')).toBeNull();
    expect(mocks.toastSuccess).toHaveBeenCalledTimes(1);
    expect(mocks.toastSuccess.mock.calls[0]![0]).toBe('Log cleared');

    const toastOptions = mocks.toastSuccess.mock.calls[0]![1] as {
      action: { label: string; onClick: () => void };
    };
    expect(toastOptions.action.label).toBe('Undo');

    act(() => {
      toastOptions.action.onClick();
    });
    expect(screen.getByText('daemon ready')).toBeTruthy();
  });
});
