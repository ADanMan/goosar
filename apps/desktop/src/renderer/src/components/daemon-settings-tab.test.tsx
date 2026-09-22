import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import type {
  AgentConfigSaveResult,
  AgentRuntimeStatus,
} from '../../../shared/agent-runtime-types';
import type { LlmRuntimeSaveResult } from '../../../shared/llm-runtime-settings';

const mocks = vi.hoisted(() => ({
  getAgentRuntime: vi.fn(),
  getAgentRunner: vi.fn(),
  setAgentRunner: vi.fn(),
  setAgentRuntimeConfig: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock('sonner', () => ({
  toast: { success: mocks.toastSuccess, error: mocks.toastError },
}));

vi.mock('@goosar/views/i18n', async () => {
  const { settingsI18nMock } = await import('../../../../test/i18n-settings-mock');
  return await settingsI18nMock();
});

vi.mock('../platform/daemon-reauth', () => ({
  reauthenticateDaemon: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('@goosar/core/auth', () => ({
  useAuthStore: (selector: (s: { user: { email: string } }) => unknown) =>
    selector({ user: { email: 'user@example.test' } }),
}));

import { DaemonSettingsTab } from './daemon-settings-tab';

const CONFIG_PATH = '/home/u/.hermes/config.user.yaml';

const FRESH_INSTALL: AgentRuntimeStatus = {
  state: 'needs_config',
  version: '2026.5.5',
  binPath: '/home/u/.hermes/runtime/current/bin/hermes',
  configPath: CONFIG_PATH,
  missing: ['llm.api_base', 'llm.api_key'],
};

const READY: AgentRuntimeStatus = {
  state: 'ready',
  version: '2026.5.5',
  binPath: '/home/u/.hermes/runtime/current/bin/hermes',
  configPath: CONFIG_PATH,
};

const GATEWAY = 'https://gw.corp.example/v1';
const SECRET = 'sk-super-secret-value';

function fillAndSave(values: { gateway?: string; model?: string; key?: string }) {
  if (values.gateway !== undefined) {
    fireEvent.change(screen.getByLabelText('Gateway address'), {
      target: { value: values.gateway },
    });
  }
  if (values.model !== undefined) {
    fireEvent.change(screen.getByLabelText('Model'), {
      target: { value: values.model },
    });
  }
  if (values.key !== undefined) {
    fireEvent.change(screen.getByLabelText('API key'), {
      target: { value: values.key },
    });
  }
  fireEvent.click(screen.getByRole('button', { name: 'Save settings' }));
}

beforeEach(() => {
  mocks.getAgentRuntime.mockReset().mockResolvedValue(FRESH_INSTALL);
  mocks.setAgentRuntimeConfig.mockReset();
  mocks.getAgentRunner
    .mockReset()
    .mockResolvedValue({ choice: 'none', bundledAvailable: true, bundledVersion: '1.0.0' });
  mocks.setAgentRunner.mockReset();
  mocks.toastSuccess.mockReset();
  mocks.toastError.mockReset();

  Object.defineProperty(window, 'daemonAPI', {
    configurable: true,
    value: {
      getPrefs: vi.fn().mockResolvedValue({ autoStart: true, autoStop: false }),
      setPrefs: vi.fn(),
      isCliInstalled: vi.fn().mockResolvedValue(true),
      getStatus: vi.fn().mockResolvedValue({ state: 'running' }),
      onStatusChange: vi.fn().mockReturnValue(() => {}),
      getAgentRuntime: mocks.getAgentRuntime,
      getAgentRunner: mocks.getAgentRunner,
      setAgentRunner: mocks.setAgentRunner,
      setAgentRuntimeConfig: mocks.setAgentRuntimeConfig,
    },
  });
  Object.defineProperty(window, 'desktopAPI', {
    configurable: true,
    value: { openExternal: vi.fn() },
  });
});

describe('DaemonSettingsTab agent runner', () => {
  it('defaults to no runner and opts in to the bundled one on request', async () => {
    mocks.setAgentRunner.mockResolvedValue({ success: true, choice: 'hermes', runtime: READY });
    render(<DaemonSettingsTab />);

    const toggle = await screen.findByRole('switch', { name: 'Use the bundled runner' });
    await waitFor(() => expect(toggle.getAttribute('aria-checked')).toBe('false'));
    fireEvent.click(toggle);

    await waitFor(() => expect(mocks.setAgentRunner).toHaveBeenCalledWith('hermes'));
  });

  it('disables the choice when this build carries no bundled runner', async () => {
    mocks.getAgentRunner.mockResolvedValue({
      choice: 'none',
      bundledAvailable: false,
      bundledVersion: null,
    });
    render(<DaemonSettingsTab />);

    expect(await screen.findByText(/does not include a bundled runner/)).toBeTruthy();
    const toggle = screen.getByRole('switch', { name: 'Use the bundled runner' });
    expect(
      toggle.hasAttribute('data-disabled') || toggle.getAttribute('aria-disabled') === 'true',
    ).toBe(true);
  });
});

describe('DaemonSettingsTab agent setup', () => {
  it('names what is missing instead of only saying setup is needed', async () => {
    render(<DaemonSettingsTab />);

    expect(await screen.findByText(/Gateway address/)).toBeTruthy();
    expect(screen.getByText(new RegExp(CONFIG_PATH.replace(/\./g, '\\.')))).toBeTruthy();
    expect(screen.getByText(/still needs/)).toBeTruthy();
  });

  it('does not offer the form once the runtime is configured', async () => {
    mocks.getAgentRuntime.mockResolvedValue(READY);
    render(<DaemonSettingsTab />);

    await screen.findByText(/is installed and configured/);
    expect(screen.queryByLabelText('API key')).toBeNull();
  });

  it('explains that there is nothing to write into when no config file exists', async () => {
    mocks.getAgentRuntime.mockResolvedValue({
      ...FRESH_INSTALL,
      configPath: null,
      missing: ['llm.api_base', 'llm.model', 'llm.api_key'],
    });
    render(<DaemonSettingsTab />);

    await screen.findByText(/there is no config file/);
    expect(screen.queryByRole('button', { name: 'Save settings' })).toBeNull();
  });

  it('sends only the fields the user filled in', async () => {
    mocks.setAgentRuntimeConfig.mockResolvedValue({
      ok: true,
      path: CONFIG_PATH,
      shadowed: [],
    } satisfies AgentConfigSaveResult);
    render(<DaemonSettingsTab />);
    await screen.findByLabelText('API key');

    fillAndSave({ gateway: GATEWAY, key: SECRET });

    await waitFor(() =>
      expect(mocks.setAgentRuntimeConfig).toHaveBeenCalledWith({
        'llm.api_base': GATEWAY,
        'llm.api_key': SECRET,
      }),
    );
  });

  it('drops the key from the form as soon as the call returns', async () => {
    mocks.setAgentRuntimeConfig.mockResolvedValue({
      ok: true,
      path: CONFIG_PATH,
      shadowed: [],
    } satisfies AgentConfigSaveResult);
    render(<DaemonSettingsTab />);
    const keyInput = (await screen.findByLabelText('API key')) as HTMLInputElement;

    fillAndSave({ gateway: GATEWAY, key: SECRET });

    await waitFor(() => expect(keyInput.value).toBe(''));
    expect((screen.getByLabelText('Gateway address') as HTMLInputElement).value).toBe(GATEWAY);
    await waitFor(() => expect(mocks.getAgentRuntime).toHaveBeenCalledTimes(2));
  });

  it('never renders the key back to the user', async () => {
    mocks.setAgentRuntimeConfig.mockResolvedValue({
      ok: true,
      path: CONFIG_PATH,
      shadowed: [],
    } satisfies AgentConfigSaveResult);
    const { container } = render(<DaemonSettingsTab />);
    await screen.findByLabelText('API key');

    fillAndSave({ key: SECRET });

    await waitFor(() => expect(mocks.setAgentRuntimeConfig).toHaveBeenCalled());
    await waitFor(() => expect(container.innerHTML).not.toContain(SECRET));
  });

  it('masks the key field so it is not readable over a shoulder', async () => {
    render(<DaemonSettingsTab />);
    const keyInput = (await screen.findByLabelText('API key')) as HTMLInputElement;
    expect(keyInput.type).toBe('password');
    expect((screen.getByLabelText('Gateway address') as HTMLInputElement).type).toBe('text');
  });

  it('warns when the write landed but config.runtime.yaml overrides it', async () => {
    mocks.setAgentRuntimeConfig.mockResolvedValue({
      ok: true,
      path: CONFIG_PATH,
      shadowed: ['llm.api_base'],
    } satisfies AgentConfigSaveResult);
    render(<DaemonSettingsTab />);
    await screen.findByLabelText('API key');

    fillAndSave({ gateway: GATEWAY, key: SECRET });

    const warning = await screen.findByText(/config\.runtime\.yaml/);
    expect(warning.textContent).toContain('Gateway address');
    expect(warning.textContent).toContain('llm.api_base');
  });

  it('keeps the shadowed warning on screen after the runtime reports itself ready', async () => {
    mocks.getAgentRuntime.mockReset().mockResolvedValueOnce(FRESH_INSTALL).mockResolvedValue(READY);
    mocks.setAgentRuntimeConfig.mockResolvedValue({
      ok: true,
      path: CONFIG_PATH,
      shadowed: ['llm.api_base'],
    } satisfies AgentConfigSaveResult);
    render(<DaemonSettingsTab />);
    await screen.findByLabelText('API key');

    fillAndSave({ gateway: GATEWAY, key: SECRET });

    await waitFor(() => expect(screen.queryByLabelText('API key')).toBeNull());
    expect(screen.getByText(/config\.runtime\.yaml/)).toBeTruthy();
    expect(screen.getByText(/Saved, but not in effect/)).toBeTruthy();
  });

  it("shows the runtime's own reason when the value is rejected", async () => {
    mocks.setAgentRuntimeConfig.mockResolvedValue({
      ok: false,
      field: 'llm.api_base',
      kind: 'invalid_value',
      message: 'llm.api_base: api_base must be a valid URL',
    } satisfies AgentConfigSaveResult);
    render(<DaemonSettingsTab />);
    await screen.findByLabelText('API key');

    fillAndSave({ gateway: 'gateway.corp', key: SECRET });

    await screen.findByText(/api_base must be a valid URL/);
    expect(mocks.toastSuccess).not.toHaveBeenCalled();
  });

  it('names the fields already written when a later one is rejected', async () => {
    mocks.setAgentRuntimeConfig.mockResolvedValue({
      ok: false,
      field: 'llm.model',
      kind: 'invalid_value',
      message: 'llm.model: unknown model id',
    } satisfies AgentConfigSaveResult);
    render(<DaemonSettingsTab />);
    await screen.findByLabelText('API key');

    fillAndSave({ gateway: GATEWAY, model: 'bogus/model', key: SECRET });

    const banner = await screen.findByText(/Saved part of the way/);
    expect(screen.getByText(/unknown model id/)).toBeTruthy();
    expect(screen.getByText(/Gateway address was already written/)).toBeTruthy();
    expect(banner.parentElement?.textContent).not.toMatch(/Nothing was changed/);
  });

  it('still says nothing changed when the rejected field is the first one', async () => {
    mocks.setAgentRuntimeConfig.mockResolvedValue({
      ok: false,
      field: 'llm.api_base',
      kind: 'invalid_value',
      message: 'llm.api_base: api_base must be a valid URL',
    } satisfies AgentConfigSaveResult);
    render(<DaemonSettingsTab />);
    await screen.findByLabelText('API key');

    fillAndSave({ gateway: 'gateway.corp', model: 'openai/glm-4.6', key: SECRET });

    await screen.findByText('Nothing was changed');
    expect(screen.queryByText(/already written/)).toBeNull();
  });

  it('survives an IPC rejection without losing the form', async () => {
    mocks.setAgentRuntimeConfig.mockRejectedValue(new Error('channel closed'));
    render(<DaemonSettingsTab />);
    await screen.findByLabelText('API key');

    fillAndSave({ gateway: GATEWAY, key: SECRET });

    await screen.findByText(/channel closed/);
    expect(screen.getByLabelText('API key')).toBeTruthy();
  });

  it('claims nothing about the config when the call never answered', async () => {
    mocks.setAgentRuntimeConfig.mockRejectedValue(new Error('channel closed'));
    render(<DaemonSettingsTab />);
    await screen.findByLabelText('API key');

    fillAndSave({ gateway: GATEWAY, key: SECRET });

    await screen.findByText(/may or may not have been changed/);
    expect(screen.queryByText('Nothing was changed')).toBeNull();
  });
});

describe('DaemonSettingsTab model connection (#160)', () => {
  const llmMocks = vi.hoisted(() => ({
    getLlmRuntimeConfig: vi.fn(),
    saveLlmRuntimeConfig: vi.fn(),
  }));

  const CURRENT_MODEL = 'openai/glm-4.6';
  const CURRENT_BASE = 'https://gw.corp.example/v1';

  beforeEach(() => {
    mocks.getAgentRuntime.mockReset().mockResolvedValue(READY);
    llmMocks.getLlmRuntimeConfig.mockReset().mockResolvedValue({
      apiBase: CURRENT_BASE,
      model: CURRENT_MODEL,
      hasKey: true,
      configPath: CONFIG_PATH,
    });
    llmMocks.saveLlmRuntimeConfig.mockReset();

    Object.defineProperty(window, 'daemonAPI', {
      configurable: true,
      value: {
        getPrefs: vi.fn().mockResolvedValue({ autoStart: true, autoStop: false }),
        setPrefs: vi.fn(),
        isCliInstalled: vi.fn().mockResolvedValue(true),
        getStatus: vi.fn().mockResolvedValue({ state: 'running' }),
        onStatusChange: vi.fn().mockReturnValue(() => {}),
        getAgentRuntime: mocks.getAgentRuntime,
        getAgentRunner: mocks.getAgentRunner,
        setAgentRunner: mocks.setAgentRunner,
        setAgentRuntimeConfig: mocks.setAgentRuntimeConfig,
        getLlmRuntimeConfig: llmMocks.getLlmRuntimeConfig,
        saveLlmRuntimeConfig: llmMocks.saveLlmRuntimeConfig,
      },
    });
  });

  it('shows the current model and gateway once the runtime is ready', async () => {
    render(<DaemonSettingsTab />);

    const model = (await screen.findByLabelText('Model')) as HTMLInputElement;
    const base = screen.getByLabelText('Gateway address') as HTMLInputElement;
    expect(model.value).toBe(CURRENT_MODEL);
    expect(base.value).toBe(CURRENT_BASE);
  });

  it('saves the edited model/endpoint through the write path and renders reachability', async () => {
    llmMocks.saveLlmRuntimeConfig.mockResolvedValue({
      status: 'written',
      saved: { ok: true, path: CONFIG_PATH, shadowed: [] },
      reachability: { kind: 'response', status: 200 },
    } satisfies LlmRuntimeSaveResult);
    render(<DaemonSettingsTab />);

    const model = (await screen.findByLabelText('Model')) as HTMLInputElement;
    fireEvent.change(model, { target: { value: 'openai/gpt-5' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save and check' }));

    await waitFor(() =>
      expect(llmMocks.saveLlmRuntimeConfig).toHaveBeenCalledWith({
        apiBase: CURRENT_BASE,
        model: 'openai/gpt-5',
      }),
    );
    await screen.findByText(/Endpoint reachable/);
    expect(screen.getByText(/HTTP 200/)).toBeTruthy();
  });

  it('surfaces an unreachable endpoint after saving', async () => {
    llmMocks.saveLlmRuntimeConfig.mockResolvedValue({
      status: 'written',
      saved: { ok: true, path: CONFIG_PATH, shadowed: [] },
      reachability: { kind: 'unreachable' },
    } satisfies LlmRuntimeSaveResult);
    render(<DaemonSettingsTab />);

    await screen.findByLabelText('Model');
    fireEvent.click(screen.getByRole('button', { name: 'Save and check' }));

    await screen.findByText(/Endpoint unreachable/);
  });

  it("shows the runtime's own reason when the value is rejected", async () => {
    llmMocks.saveLlmRuntimeConfig.mockResolvedValue({
      status: 'written',
      saved: {
        ok: false,
        field: 'llm.model',
        kind: 'invalid_value',
        message: 'llm.model: unknown model id',
      },
      reachability: null,
    } satisfies LlmRuntimeSaveResult);
    render(<DaemonSettingsTab />);

    await screen.findByLabelText('Model');
    fireEvent.click(screen.getByRole('button', { name: 'Save and check' }));

    await screen.findByText(/unknown model id/);
    expect(mocks.toastSuccess).not.toHaveBeenCalled();
  });

  it('renders nothing when the preload predates the API', async () => {
    Object.defineProperty(window, 'daemonAPI', {
      configurable: true,
      value: {
        getPrefs: vi.fn().mockResolvedValue({ autoStart: true, autoStop: false }),
        setPrefs: vi.fn(),
        isCliInstalled: vi.fn().mockResolvedValue(true),
        getStatus: vi.fn().mockResolvedValue({ state: 'running' }),
        onStatusChange: vi.fn().mockReturnValue(() => {}),
        getAgentRuntime: mocks.getAgentRuntime,
        getAgentRunner: mocks.getAgentRunner,
        setAgentRunner: mocks.setAgentRunner,
        // getLlmRuntimeConfig / saveLlmRuntimeConfig intentionally absent.
      },
    });
    render(<DaemonSettingsTab />);

    await screen.findByText(/is installed and configured/);
    expect(screen.queryByLabelText('Model')).toBeNull();
  });
});
