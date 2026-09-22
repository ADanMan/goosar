import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enOnboarding from '../../locales/en/onboarding.json';
import { configStore, EMPTY_DEPLOYMENT_HOSTS } from '@goosar/core/config';
import { getLlmConnectionPreset } from '../presets';
import {
  LlmConnectionForm,
  type LlmConnectionSaveResult,
  type LlmConnectionValues,
} from './llm-connection-form';

const mockToastError = vi.fn();
vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: (...args: unknown[]) => mockToastError(...args) },
}));

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

const DEPLOYMENT_HOSTS = {
  ...EMPTY_DEPLOYMENT_HOSTS,
  llmApiBase: 'https://llm.example.test/api/v3',
  llmModel: 'openai/coding-medium',
};

beforeEach(() => {
  configStore.setState({ deploymentHosts: DEPLOYMENT_HOSTS });
});

afterEach(() => {
  configStore.setState({ deploymentHosts: EMPTY_DEPLOYMENT_HOSTS });
});

const PERIMETER = getLlmConnectionPreset('perimeter', DEPLOYMENT_HOSTS)!;
const OUTSIDE = getLlmConnectionPreset('outside', DEPLOYMENT_HOSTS)!;

function renderForm(
  onSave: (values: LlmConnectionValues) => Promise<LlmConnectionSaveResult>,
  props: Partial<Parameters<typeof LlmConnectionForm>[0]> = {},
) {
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <LlmConnectionForm onSave={onSave} {...props} />
    </I18nProvider>,
  );
}

function apiKeyField() {
  return screen.getByLabelText(/access key/i);
}

function presetRadio(name: RegExp) {
  return screen.getByRole('radio', { name });
}

function customFields() {
  return {
    apiBase: screen.getByLabelText(/service address/i),
    model: screen.getByLabelText(/^model$/i),
  };
}

describe('LlmConnectionForm', () => {
  it('renders the preset picker with the first preset selected and only the key field', () => {
    renderForm(vi.fn());

    expect(presetRadio(/^perimeter$/i)).toHaveAttribute('aria-checked', 'true');
    expect(presetRadio(/outside the perimeter/i)).toHaveAttribute('aria-checked', 'false');
    expect(presetRadio(/^other$/i)).toHaveAttribute('aria-checked', 'false');

    expect(apiKeyField()).toBeInTheDocument();
    expect(screen.queryByLabelText(/service address/i)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/^model$/i)).not.toBeInTheDocument();
  });

  it("preselects the preset the machine's perimeter toggle asks for", () => {
    renderForm(vi.fn(), { defaultChoice: 'outside' });

    expect(presetRadio(/outside the perimeter/i)).toHaveAttribute('aria-checked', 'true');
    expect(presetRadio(/^perimeter$/i)).toHaveAttribute('aria-checked', 'false');
  });

  it('flags a missing corporate CA bundle on the perimeter preset only', async () => {
    const user = userEvent.setup();
    renderForm(vi.fn(), { perimeterCaMissing: true });

    expect(screen.getByText(/corporate ca bundle is missing/i)).toBeInTheDocument();

    await user.click(presetRadio(/outside the perimeter/i));
    expect(screen.queryByText(/corporate ca bundle is missing/i)).not.toBeInTheDocument();
  });

  it('shows no CA hint when the machine reports nothing', () => {
    renderForm(vi.fn());
    expect(screen.queryByText(/corporate ca bundle is missing/i)).not.toBeInTheDocument();
  });

  it("shows the selected preset's endpoint and model instead of hiding them", () => {
    renderForm(vi.fn());
    expect(screen.getByText(new RegExp(PERIMETER.apiBase))).toBeInTheDocument();
    expect(screen.getByText(new RegExp(PERIMETER.model))).toBeInTheDocument();
  });

  it("saves the selected preset's endpoint and model with the typed key", async () => {
    const user = userEvent.setup();
    const onSave = vi
      .fn<(values: LlmConnectionValues) => Promise<LlmConnectionSaveResult>>()
      .mockResolvedValue({ ok: true });
    renderForm(onSave);

    await user.click(presetRadio(/outside the perimeter/i));
    await user.type(apiKeyField(), 'sk-secret-value');
    await user.click(screen.getByRole('button', { name: /save connection/i }));

    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith({
        apiBase: OUTSIDE.apiBase,
        model: OUTSIDE.model,
        apiKey: 'sk-secret-value',
      }),
    );
    expect(await screen.findByText(/connection saved/i)).toBeInTheDocument();
  });

  it('keeps Save disabled with a preset selected until the key is present', async () => {
    const user = userEvent.setup();
    renderForm(vi.fn());
    const save = screen.getByRole('button', { name: /save connection/i });

    expect(save).toBeDisabled();
    await user.type(apiKeyField(), 'sk-secret-value');
    expect(save).toBeEnabled();
  });

  it('reveals the manual address and model fields when Other is picked', async () => {
    const user = userEvent.setup();
    renderForm(vi.fn());

    await user.click(presetRadio(/^other$/i));

    const { apiBase, model } = customFields();
    expect(apiBase).toBeInTheDocument();
    expect(model).toBeInTheDocument();
  });

  it('keeps Save disabled in manual mode until all three values are present', async () => {
    const user = userEvent.setup();
    renderForm(vi.fn());
    await user.click(presetRadio(/^other$/i));
    const save = screen.getByRole('button', { name: /save connection/i });
    expect(save).toBeDisabled();

    await user.type(customFields().apiBase, 'https://llm.example.com/v1');
    expect(save).toBeDisabled();

    await user.type(customFields().model, 'openai/glm-5.1');
    expect(save).toBeDisabled();

    await user.type(apiKeyField(), 'sk-secret-value');
    expect(save).toBeEnabled();
  });

  it('saves the manually entered values in Other mode', async () => {
    const user = userEvent.setup();
    const onSave = vi
      .fn<(values: LlmConnectionValues) => Promise<LlmConnectionSaveResult>>()
      .mockResolvedValue({ ok: true });
    renderForm(onSave);

    await user.click(presetRadio(/^other$/i));
    await user.type(customFields().apiBase, 'https://example.test/v1');
    await user.type(customFields().model, 'openai/custom-model');
    await user.type(apiKeyField(), 'sk-secret-value');
    await user.click(screen.getByRole('button', { name: /save connection/i }));

    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith({
        apiBase: 'https://example.test/v1',
        model: 'openai/custom-model',
        apiKey: 'sk-secret-value',
      }),
    );
  });

  it('masks the API key and only reveals it on an explicit toggle', async () => {
    const user = userEvent.setup();
    renderForm(vi.fn());

    expect(apiKeyField()).toHaveAttribute('type', 'password');

    await user.click(screen.getByRole('button', { name: /show the key/i }));
    expect(apiKeyField()).toHaveAttribute('type', 'text');

    await user.click(screen.getByRole('button', { name: /hide the key/i }));
    expect(apiKeyField()).toHaveAttribute('type', 'password');
  });

  it('drops the API key from the field once the save lands', async () => {
    const user = userEvent.setup();
    renderForm(vi.fn().mockResolvedValue({ ok: true }));

    await user.type(apiKeyField(), 'sk-secret-value');
    await user.click(screen.getByRole('button', { name: /save connection/i }));

    await waitFor(() => expect(apiKeyField()).toHaveValue(''));
    expect(apiKeyField()).toHaveAttribute('type', 'password');
  });

  it('warns instead of claiming success when the write cannot take effect', async () => {
    const user = userEvent.setup();
    renderForm(vi.fn().mockResolvedValue({ ok: true, ineffective: true }));

    await user.type(apiKeyField(), 'sk-secret-value');
    await user.click(screen.getByRole('button', { name: /save connection/i }));

    expect(
      await screen.findByText(/something else on this computer overrides them/i),
    ).toBeInTheDocument();
    expect(screen.queryByText(/your agent can use this model now/i)).toBeNull();
  });

  it("surfaces the platform's own explanation when the save fails", async () => {
    const user = userEvent.setup();
    renderForm(
      vi.fn().mockResolvedValue({
        ok: false,
        message: 'hermes rejected that value, so it was not written.',
      }),
    );

    await user.type(apiKeyField(), 'sk-secret-value');
    await user.click(screen.getByRole('button', { name: /save connection/i }));

    expect(await screen.findByText(/hermes rejected that value/i)).toBeInTheDocument();
    expect(screen.queryByText(/connection saved/i)).not.toBeInTheDocument();
  });

  it('keeps the typed key and shows a reminder when switching between presets', async () => {
    const user = userEvent.setup();
    renderForm(vi.fn());

    await user.type(apiKeyField(), 'sk-perimeter-key');
    await user.click(presetRadio(/outside the perimeter/i));

    expect(apiKeyField()).toHaveValue('sk-perimeter-key');
    expect(
      screen.getByText(/make sure it is the key for outside the perimeter/i),
    ).toBeInTheDocument();
  });

  it('drops the preset-switch reminder as soon as the key is edited', async () => {
    const user = userEvent.setup();
    renderForm(vi.fn());

    await user.type(apiKeyField(), 'sk-perimeter-key');
    await user.click(presetRadio(/outside the perimeter/i));
    expect(screen.getByText(/make sure it is the key for/i)).toBeInTheDocument();

    await user.type(apiKeyField(), 'x');
    expect(screen.queryByText(/make sure it is the key for/i)).toBeNull();
  });

  it('shows no preset-switch reminder without a key or when manual mode is involved', async () => {
    const user = userEvent.setup();
    renderForm(vi.fn());

    await user.click(presetRadio(/outside the perimeter/i));
    expect(screen.queryByText(/make sure it is the key for/i)).toBeNull();

    await user.type(apiKeyField(), 'sk-some-key');
    await user.click(presetRadio(/^other$/i));
    expect(screen.queryByText(/make sure it is the key for/i)).toBeNull();

    await user.click(presetRadio(/^perimeter$/i));
    expect(screen.queryByText(/make sure it is the key for/i)).toBeNull();
  });

  it('falls back to a translated error when the call throws', async () => {
    const user = userEvent.setup();
    renderForm(vi.fn().mockRejectedValue(new Error('EPIPE /Users/me/key.txt')));

    await user.type(apiKeyField(), 'sk-secret-value');
    await user.click(screen.getByRole('button', { name: /save connection/i }));

    expect(await screen.findByText(/the connection could not be saved/i)).toBeInTheDocument();
    expect(screen.queryByText(/EPIPE/)).not.toBeInTheDocument();
  });

  describe('server origin note', () => {
    it('shows where an unlocked server connection comes from', () => {
      renderForm(vi.fn(), {
        serverConnection: {
          origin: 'workspace',
          locked: false,
          hasApiKey: true,
        },
      });

      const note = screen.getByTestId('llm-server-origin');
      expect(note).toHaveTextContent(/set for your workspace/i);
      expect(note).toHaveTextContent(/wins over it for this computer only/i);
      expect(note).not.toHaveTextContent(/will not take effect/i);
    });

    it('warns that a locked policy connection overrides local saves', () => {
      renderForm(vi.fn(), {
        serverConnection: { origin: 'policy', locked: true },
      });

      const note = screen.getByTestId('llm-server-origin');
      expect(note).toHaveTextContent(/set for the whole deployment/i);
      expect(note).toHaveTextContent(/will not take effect/i);
    });

    it('renders an origin this build has never heard of with the generic label', () => {
      renderForm(vi.fn(), {
        serverConnection: { origin: 'regional_policy', locked: false },
      });

      expect(screen.getByTestId('llm-server-origin')).toHaveTextContent(/set on the server/i);
    });

    it('renders nothing without server config, exactly as before', () => {
      renderForm(vi.fn());
      expect(screen.queryByTestId('llm-server-origin')).toBeNull();

      renderForm(vi.fn(), { serverConnection: null });
      expect(screen.queryByTestId('llm-server-origin')).toBeNull();
    });
  });
});

describe('existing local connection (issue #258)', () => {
  const EXISTING = {
    apiBase: 'https://gw.internal/v1',
    model: 'openai/coding-medium',
    hasKey: true,
  };

  it('shows what is already configured instead of blank fields', () => {
    renderForm(vi.fn(), { existingConnection: EXISTING });

    expect(screen.getByText('https://gw.internal/v1')).toBeTruthy();
    expect(screen.getByText('openai/coding-medium')).toBeTruthy();
  });

  it('offers nothing that could overwrite the existing connection', () => {
    renderForm(vi.fn(), { existingConnection: EXISTING });

    expect(screen.queryByRole('button', { name: /^save/i })).toBeNull();
    expect(screen.queryByLabelText(/access key/i)).toBeNull();
  });

  it('still lets the user change it deliberately', async () => {
    const onSave = vi.fn(async () => ({ ok: true }) as LlmConnectionSaveResult);
    renderForm(onSave, { existingConnection: EXISTING });

    await userEvent.click(screen.getByRole('button', { name: /change/i }));

    expect(apiKeyField()).toBeTruthy();
    expect((apiKeyField() as HTMLInputElement).value).toBe('');
  });

  it('renders the plain form when there is nothing configured', () => {
    renderForm(vi.fn(), { existingConnection: null });

    expect(apiKeyField()).toBeTruthy();
    expect(screen.queryByRole('button', { name: /change/i })).toBeNull();
  });
});

describe('key guidance and policy-supplied keys (issue #438)', () => {
  it("hides the guide until asked, then shows the selected preset's steps", async () => {
    const user = userEvent.setup();
    renderForm(vi.fn());

    expect(screen.queryByTestId('llm-key-guide')).toBeNull();
    await user.click(screen.getByRole('button', { name: /where do i get the key/i }));

    expect(screen.getByTestId('llm-key-guide')).toHaveTextContent(/corporate AI portal/i);
  });

  it('swaps the guide when another preset is picked', async () => {
    const user = userEvent.setup();
    renderForm(vi.fn());

    await user.click(screen.getByRole('button', { name: /where do i get the key/i }));
    await user.click(presetRadio(/outside the perimeter/i));

    expect(screen.getByTestId('llm-key-guide')).toHaveTextContent(/llm\.example\.com/i);
  });

  it('asks for nothing when the deployment policy already supplies a key', () => {
    renderForm(vi.fn(), {
      serverConnection: { origin: 'policy', locked: true, hasApiKey: true },
    });

    expect(screen.queryByLabelText(/access key/i)).toBeNull();
    expect(screen.queryByRole('radiogroup')).toBeNull();
    expect(screen.getByText(enOnboarding.step_runtime.llm.server_key_body)).toBeInTheDocument();
  });

  it('still asks for a key when the server layer has none', () => {
    renderForm(vi.fn(), {
      serverConnection: { origin: 'policy', locked: true, hasApiKey: false },
    });

    expect(apiKeyField()).toBeInTheDocument();
  });

  it('still offers the form when the server key is not locked', () => {
    renderForm(vi.fn(), {
      serverConnection: { origin: 'policy', locked: false, hasApiKey: true },
    });

    expect(apiKeyField()).toBeInTheDocument();
    expect(screen.getByTestId('llm-server-origin')).toHaveTextContent(
      enOnboarding.step_runtime.llm.server_origin_unlocked,
    );
  });
});

describe('a preset this deployment does not offer', () => {
  it('falls back to an offered one instead of an unchecked picker', async () => {
    configStore.setState({ deploymentHosts: EMPTY_DEPLOYMENT_HOSTS });
    const onSave = vi
      .fn<(values: LlmConnectionValues) => Promise<LlmConnectionSaveResult>>()
      .mockResolvedValue({ ok: true });
    renderForm(onSave, { defaultChoice: 'perimeter' });

    expect(screen.queryByRole('radio', { name: /^perimeter$/i })).toBeNull();
    expect(screen.getByRole('radio', { name: /^outside the perimeter$/i })).toBeChecked();

    await userEvent.type(screen.getByLabelText(/access key/i), 'sk-test');
    await userEvent.click(screen.getByRole('button', { name: /connect/i }));
    await waitFor(() => expect(onSave).toHaveBeenCalled());
    expect(onSave.mock.calls[0]![0].apiBase).toBe(OUTSIDE.apiBase);
  });
});
