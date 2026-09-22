import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import { UninstallSection } from './uninstall-section';
import type {
  UninstallItem,
  UninstallOutcome,
  UninstallPlan,
} from '../../../shared/uninstall-types';

const RUNTIME = {
  id: 'agent-runtime',
  path: '/home/u/.hermes/runtime',
  label: 'Agent runtime — the bundled hermes interpreter and every version of it',
  kind: 'software',
  bytes: 382_730_240,
} as const;

const APP_DATA = {
  id: 'app-data',
  path: '/home/u/Library/Application Support/Goosar',
  label: 'Desktop app data — window layout, caches, and the managed goosar CLI',
  kind: 'software',
  bytes: 12_582_912,
} as const;

const CONFIG = {
  id: 'agent-config',
  path: '/home/u/.hermes/config.user.yaml',
  label: 'Agent configuration, including your LLM API key',
  kind: 'user_data',
  bytes: 412,
} as const;

const AGENTS = {
  id: 'agent-agents',
  path: '/home/u/.hermes/agents',
  label: 'Agents you wrote',
  kind: 'user_data',
  bytes: 88_064,
} as const;

const FOREIGN_BIN = '/home/u/.local/bin/hermes';
const BUNDLE = '/Applications/Goosar.app';

const PLAN: UninstallPlan = {
  software: [RUNTIME, APP_DATA],
  userData: [CONFIG, AGENTS],
  kept: [
    {
      path: FOREIGN_BIN,
      reason: 'A hermes command that this app did not install. It is left exactly as it is.',
    },
  ],
  manualSteps: [
    {
      path: BUNDLE,
      instruction:
        'macOS does not let an app delete itself while it is running. Quit Goosar, then move this to the Trash.',
    },
  ],
};

const REMOVED_OUTCOME: UninstallOutcome = {
  status: 'removed',
  daemon: { stopped: true },
  removed: [RUNTIME, APP_DATA],
  failed: [],
  kept: PLAN.kept,
  manualSteps: PLAN.manualSteps,
};

const mocks = vi.hoisted(() => ({
  planUninstall: vi.fn(),
  performUninstall: vi.fn(),
}));

beforeEach(() => {
  mocks.planUninstall.mockReset().mockResolvedValue(PLAN);
  mocks.performUninstall.mockReset().mockResolvedValue(REMOVED_OUTCOME);
  Object.defineProperty(window, 'desktopAPI', {
    configurable: true,
    value: {
      planUninstall: mocks.planUninstall,
      performUninstall: mocks.performUninstall,
    },
  });
});

function showTheList(): void {
  fireEvent.click(screen.getByRole('button', { name: /show what would be removed/i }));
}

function removeButton(): HTMLElement {
  return screen.getByRole('button', { name: /^Remove / });
}

describe('UninstallSection', () => {
  it('offers nothing destructive before the list has been read', () => {
    render(<UninstallSection />);

    expect(screen.queryByRole('button', { name: /^Remove / })).toBeNull();
    expect(mocks.planUninstall).not.toHaveBeenCalled();
  });

  it('names every path and its size before anything is removed', async () => {
    render(<UninstallSection />);
    showTheList();

    expect(await screen.findByText(RUNTIME.path)).toBeTruthy();
    expect(screen.getByText(APP_DATA.path)).toBeTruthy();
    expect(screen.getByText('365.0 MB')).toBeTruthy();
    expect(screen.getByText('12.0 MB')).toBeTruthy();
    expect(screen.getByText(CONFIG.path)).toBeTruthy();
    expect(mocks.performUninstall).not.toHaveBeenCalled();
  });

  it('counts and sizes only the software until the opt-in is ticked', async () => {
    render(<UninstallSection />);
    showTheList();

    await screen.findByText(RUNTIME.path);
    expect(removeButton().textContent).toContain('2 items');
    expect(removeButton().textContent).toContain('377.0 MB');

    fireEvent.click(screen.getByRole('checkbox'));

    await waitFor(() => expect(removeButton().textContent).toContain('4 items'));
    expect(removeButton().textContent).toContain('377.1 MB');
  });

  it('refuses to total a set holding a path it could not measure', async () => {
    const unmeasured: UninstallItem = {
      id: 'agent-runtime',
      path: '/home/u/.hermes/runtime',
      label: 'Agent runtime — the bundled hermes interpreter and every version of it',
      kind: 'software',
      bytes: null,
    };
    mocks.planUninstall.mockResolvedValue({
      ...PLAN,
      software: [unmeasured, APP_DATA],
    } satisfies UninstallPlan);
    render(<UninstallSection />);
    showTheList();

    await screen.findByText(unmeasured.path);

    expect(removeButton().textContent).toContain('2 items');
    expect(removeButton().textContent).toContain('unknown size');
    expect(removeButton().textContent).not.toContain('12.0 MB');
  });

  it("keeps the user's data unless the box is ticked", async () => {
    render(<UninstallSection />);
    showTheList();
    await screen.findByText(RUNTIME.path);

    fireEvent.click(removeButton());

    await waitFor(() =>
      expect(mocks.performUninstall).toHaveBeenCalledWith({
        includeUserData: false,
      }),
    );
  });

  it('passes the opt-in through once the box is ticked', async () => {
    render(<UninstallSection />);
    showTheList();
    await screen.findByText(RUNTIME.path);

    fireEvent.click(screen.getByRole('checkbox'));
    fireEvent.click(removeButton());

    await waitFor(() =>
      expect(mocks.performUninstall).toHaveBeenCalledWith({
        includeUserData: true,
      }),
    );
  });

  it("warns that the user's own files cannot be brought back", async () => {
    render(<UninstallSection />);
    showTheList();

    expect(await screen.findByText(/cannot be brought back/i)).toBeTruthy();
    expect(screen.getByText(CONFIG.label)).toBeTruthy();
  });

  it('shows what will be left alone, and why', async () => {
    render(<UninstallSection />);
    showTheList();

    expect(await screen.findByText(FOREIGN_BIN)).toBeTruthy();
    expect(screen.getByText(/did not install/i)).toBeTruthy();
  });

  it('states the one step the app cannot take for the user', async () => {
    render(<UninstallSection />);
    showTheList();

    expect(await screen.findByText(BUNDLE)).toBeTruthy();
    expect(screen.getByText(/Quit Goosar/)).toBeTruthy();
  });

  it('says nothing was removed when the daemon would not stop', async () => {
    mocks.performUninstall.mockResolvedValue({
      status: 'blocked',
      daemon: {
        stopped: false,
        detail: 'A daemon is still answering on port 19514 (pid 4242).',
      },
      removed: [],
      failed: [],
      kept: PLAN.kept,
      manualSteps: PLAN.manualSteps,
    } satisfies UninstallOutcome);
    render(<UninstallSection />);
    showTheList();
    await screen.findByText(RUNTIME.path);

    fireEvent.click(removeButton());

    expect(await screen.findByText(/Nothing was removed/i)).toBeTruthy();
    expect(screen.getByText(/pid 4242/)).toBeTruthy();
  });

  it('names the leftovers when only part of it could be removed', async () => {
    mocks.performUninstall.mockResolvedValue({
      status: 'partial',
      daemon: { stopped: true },
      removed: [APP_DATA],
      failed: [
        {
          item: RUNTIME,
          message: "EACCES: permission denied, rmdir '/home/u/.hermes/runtime'",
        },
      ],
      kept: PLAN.kept,
      manualSteps: PLAN.manualSteps,
    } satisfies UninstallOutcome);
    render(<UninstallSection />);
    showTheList();
    await screen.findByText(RUNTIME.path);

    fireEvent.click(removeButton());

    expect(await screen.findByText(/still on this computer/i)).toBeTruthy();
    expect(screen.getByText(/permission denied/)).toBeTruthy();
    expect(screen.queryByText(/^Removed everything/)).toBeNull();
  });

  it('claims nothing about the disk when the call never answered', async () => {
    mocks.performUninstall.mockRejectedValue(new Error('channel closed'));
    render(<UninstallSection />);
    showTheList();
    await screen.findByText(RUNTIME.path);

    fireEvent.click(removeButton());

    expect(await screen.findByText(/did not report back/i)).toBeTruthy();
    expect(screen.getByText(/channel closed/)).toBeTruthy();
  });

  it('says so plainly when there is nothing left to remove', async () => {
    mocks.planUninstall.mockResolvedValue({
      software: [],
      userData: [],
      kept: [],
      manualSteps: [],
    } satisfies UninstallPlan);
    render(<UninstallSection />);
    showTheList();

    expect(await screen.findByText(/nothing installed by this app/i)).toBeTruthy();
    expect(screen.queryByRole('button', { name: /^Remove / })).toBeNull();
  });
});
