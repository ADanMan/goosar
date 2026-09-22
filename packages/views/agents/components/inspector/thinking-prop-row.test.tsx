// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { RuntimeModel, RuntimeModelListRequest } from '@goosar/core/types';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../../locales/en/common.json';
import enAgents from '../../../locales/en/agents.json';
import enIssues from '../../../locales/en/issues.json';

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, issues: enIssues },
};

const mockInitiateListModels = vi.hoisted(() => vi.fn());
const mockGetListModelsResult = vi.hoisted(() => vi.fn());

vi.mock('@goosar/core/api', () => ({
  api: {
    initiateListModels: (...args: unknown[]) => mockInitiateListModels(...args),
    getListModelsResult: (...args: unknown[]) => mockGetListModelsResult(...args),
  },
}));

import { ThinkingPropRow } from './thinking-prop-row';

const CLAUDE_MODEL: RuntimeModel = {
  id: 'claude-sonnet-4-6',
  label: 'Claude Sonnet 4.6',
  default: true,
  thinking: {
    supported_levels: [
      { value: 'none', label: 'None' },
      { value: 'low', label: 'Low' },
      { value: 'medium', label: 'Medium' },
      { value: 'high', label: 'High' },
    ],
    default_level: 'medium',
  },
};

const NO_THINKING_MODEL: RuntimeModel = {
  id: 'gemini-2.5-pro',
  label: 'Gemini 2.5 Pro',
  default: true,
};

const CODEX_DEFAULT_MODEL: RuntimeModel = {
  id: 'gpt-5.6-sol',
  label: 'GPT-5.6 Sol',
  default: true,
  thinking: {
    supported_levels: [
      { value: 'low', label: 'Low' },
      { value: 'medium', label: 'Medium' },
      { value: 'high', label: 'High' },
      { value: 'max', label: 'Max' },
      { value: 'ultra', label: 'Ultra' },
    ],
    default_level: 'high',
  },
};

function listResult(models: RuntimeModel[]): RuntimeModelListRequest {
  return {
    id: 'req-1',
    runtime_id: 'runtime-1',
    status: 'completed',
    models,
    supported: true,
    created_at: '2026-05-20T00:00:00Z',
    updated_at: '2026-05-20T00:00:00Z',
  };
}

function renderRow(props: Partial<React.ComponentProps<typeof ThinkingPropRow>> = {}) {
  const onChange = vi.fn();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const utils = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <div className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5">
          <ThinkingPropRow
            runtimeId="runtime-1"
            runtimeOnline
            provider="runtime-c"
            model="claude-sonnet-4-6"
            value=""
            canEdit
            onChange={onChange}
            {...props}
          />
        </div>
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { ...utils, onChange, queryClient };
}

describe('ThinkingPropRow', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockInitiateListModels.mockResolvedValue(listResult([CLAUDE_MODEL]));
    mockGetListModelsResult.mockResolvedValue(listResult([CLAUDE_MODEL]));
  });

  afterEach(() => {
    cleanup();
  });

  it('hides the row when the active model has no thinking levels and nothing is persisted', async () => {
    mockInitiateListModels.mockResolvedValue(listResult([NO_THINKING_MODEL]));
    renderRow({ model: 'gemini-2.5-pro', value: '' });

    await waitFor(() => {
      expect(mockInitiateListModels).toHaveBeenCalled();
    });
    await waitFor(() => {
      expect(screen.queryByText('Thinking')).toBeNull();
    });
  });

  it('hides the row while the runtime is offline (no query fires)', () => {
    renderRow({ runtimeOnline: false, value: '' });

    expect(screen.queryByText('Thinking')).toBeNull();
    expect(mockInitiateListModels).not.toHaveBeenCalled();
  });

  it('renders the row with the persisted raw token when levels are empty but value is set (stale orphan)', async () => {
    mockInitiateListModels.mockResolvedValue(listResult([NO_THINKING_MODEL]));
    renderRow({ model: 'gemini-2.5-pro', value: 'xhigh' });

    await screen.findByText('Thinking');
    expect(await screen.findByText('xhigh')).toBeInTheDocument();
  });

  it('clears the orphan value via the picker footer, emitting onChange("")', async () => {
    mockInitiateListModels.mockResolvedValue(listResult([NO_THINKING_MODEL]));
    const { onChange } = renderRow({
      model: 'gemini-2.5-pro',
      value: 'xhigh',
    });

    await screen.findByText('xhigh');
    fireEvent.click(screen.getByRole('button'));
    const clearButton = await screen.findByTitle(/Clear the override/i);
    fireEvent.click(clearButton);

    expect(onChange).toHaveBeenCalledWith('');
  });

  it('renders the row with the matched label when the model still advertises the value', async () => {
    renderRow({ value: 'high' });

    await screen.findByText('Thinking');
    expect((await screen.findAllByText('High')).length).toBeGreaterThan(0);
  });

  it('renders the row with "Follow CLI config" when value is empty and the model exposes levels', async () => {
    renderRow({ value: '' });

    await screen.findByText('Thinking');
    expect((await screen.findAllByText('Follow CLI config')).length).toBeGreaterThan(0);
  });

  it("hides the picker for an empty codex model — it must not borrow the Default's catalog (MUL-4347)", async () => {
    mockInitiateListModels.mockResolvedValue(listResult([CODEX_DEFAULT_MODEL]));
    mockGetListModelsResult.mockResolvedValue(listResult([CODEX_DEFAULT_MODEL]));
    renderRow({ provider: 'runtime-e', model: '', value: '' });

    await waitFor(() => {
      expect(mockInitiateListModels).toHaveBeenCalled();
    });
    await waitFor(() => {
      expect(screen.queryByText('Thinking')).toBeNull();
    });
    expect(screen.queryByText('Ultra')).toBeNull();
  });

  it('still surfaces a persisted level on an empty codex model so it can be cleared', async () => {
    mockInitiateListModels.mockResolvedValue(listResult([CODEX_DEFAULT_MODEL]));
    mockGetListModelsResult.mockResolvedValue(listResult([CODEX_DEFAULT_MODEL]));
    const { onChange } = renderRow({
      provider: 'runtime-e',
      model: '',
      value: 'ultra',
    });

    await screen.findByText('Thinking');
    expect(await screen.findByText('ultra')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button'));
    const clearButton = await screen.findByTitle(/Clear the override/i);
    fireEvent.click(clearButton);
    expect(onChange).toHaveBeenCalledWith('');
  });

  it("still previews the Default model's levels for an empty non-codex model", async () => {
    renderRow({ provider: 'runtime-c', model: '', value: '' });

    await screen.findByText('Thinking');
    expect((await screen.findAllByText('Follow CLI config')).length).toBeGreaterThan(0);
  });
});
