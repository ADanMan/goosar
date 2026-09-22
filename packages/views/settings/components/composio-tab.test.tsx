import { describe, it, expect, beforeEach, vi } from 'vitest';
import { StrictMode } from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enSettings from '../../locales/en/settings.json';
import type { ComposioConnection, ComposioToolkit } from '@goosar/core/types';

const toolkitsRef = vi.hoisted(() => ({
  current: { data: [] as ComposioToolkit[], isLoading: false, isError: false },
}));
const connectionsRef = vi.hoisted(() => ({
  current: { data: [] as ComposioConnection[], isError: false },
}));
const searchParamsRef = vi.hoisted(() => ({ current: new URLSearchParams('tab=integrations') }));

const mockInvalidate = vi.hoisted(() => vi.fn());
const mockReplace = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());

vi.mock('@tanstack/react-query', () => ({
  useQuery: (opts: { queryKey: unknown[] }) => {
    const key = JSON.stringify(opts.queryKey);
    if (key.includes('toolkits')) return toolkitsRef.current;
    if (key.includes('connections')) return connectionsRef.current;
    return { data: undefined };
  },
  useQueryClient: () => ({ invalidateQueries: mockInvalidate }),
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock('@goosar/core/composio', () => ({
  composioKeys: {
    all: ['composio'],
    toolkits: () => ['composio', 'toolkits'],
    connections: () => ['composio', 'connections'],
  },
  composioToolkitsOptions: () => ({ queryKey: ['composio', 'toolkits'], queryFn: vi.fn() }),
  composioConnectionsOptions: () => ({ queryKey: ['composio', 'connections'], queryFn: vi.fn() }),
}));

vi.mock('@goosar/core/api', () => ({
  api: {
    beginComposioConnect: vi.fn(),
    deleteComposioConnection: vi.fn(),
  },
}));

vi.mock('../../navigation', () => ({
  useNavigation: () => ({
    push: vi.fn(),
    replace: mockReplace,
    back: vi.fn(),
    pathname: '/acme/settings',
    searchParams: searchParamsRef.current,
    getShareableUrl: (p: string) => `https://app.example${p}`,
  }),
}));

vi.mock('sonner', () => ({
  toast: { success: mockToastSuccess, error: mockToastError },
}));

import { ComposioTab } from './composio-tab';

function renderTab() {
  return render(
    <I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings } }}>
      <ComposioTab />
    </I18nProvider>,
  );
}

function renderTabStrict() {
  return render(
    <StrictMode>
      <I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings } }}>
        <ComposioTab />
      </I18nProvider>
    </StrictMode>,
  );
}

const NOTION: ComposioToolkit = {
  slug: 'notion',
  name: 'Notion',
  connectable: true,
};

beforeEach(() => {
  vi.clearAllMocks();
  toolkitsRef.current = { data: [NOTION], isLoading: false, isError: false };
  connectionsRef.current = { data: [], isError: false };
  searchParamsRef.current = new URLSearchParams('tab=integrations');
});

describe('ComposioTab', () => {
  it("renders a connected card with a 'never used' placeholder when last_used_at is null", () => {
    connectionsRef.current = {
      data: [
        {
          id: 'conn-1',
          toolkit_slug: 'notion',
          status: 'active',
          connected_at: '2026-06-01T00:00:00Z',
          last_used_at: null,
        },
      ],
      isError: false,
    };
    renderTab();
    expect(screen.getByText(enSettings.composio.connected)).toBeInTheDocument();
    expect(screen.getByText(enSettings.composio.last_used_never)).toBeInTheDocument();
  });

  it("renders a 'Last used' line when last_used_at is present", () => {
    connectionsRef.current = {
      data: [
        {
          id: 'conn-1',
          toolkit_slug: 'notion',
          status: 'active',
          connected_at: '2026-06-01T00:00:00Z',
          last_used_at: new Date(Date.now() - 2 * 60 * 1000).toISOString(),
        },
      ],
      isError: false,
    };
    renderTab();
    expect(screen.getByText(/Last used/)).toBeInTheDocument();
    expect(screen.queryByText(enSettings.composio.last_used_never)).not.toBeInTheDocument();
  });

  it('renders the expired branch with a Reconnect button', () => {
    connectionsRef.current = {
      data: [
        {
          id: 'conn-1',
          toolkit_slug: 'notion',
          status: 'expired',
          connected_at: '2026-06-01T00:00:00Z',
          last_used_at: null,
        },
      ],
      isError: false,
    };
    renderTab();
    expect(screen.getByText(enSettings.composio.expired)).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: new RegExp(enSettings.composio.reconnect) }),
    ).toBeInTheDocument();
    expect(screen.queryByText(enSettings.composio.connected)).not.toBeInTheDocument();
  });

  it('toasts success and clears the ?connected param on a successful callback', async () => {
    searchParamsRef.current = new URLSearchParams('tab=integrations&connected=notion');
    renderTab();
    await waitFor(() => {
      expect(mockToastSuccess).toHaveBeenCalledWith(enSettings.composio.toast_connected);
    });
    expect(mockInvalidate).toHaveBeenCalledWith({ queryKey: ['composio', 'connections'] });
    expect(mockReplace).toHaveBeenCalledWith('/acme/settings?tab=integrations');
  });

  it('toasts error on a failed callback', async () => {
    searchParamsRef.current = new URLSearchParams('tab=integrations&error=composio_connect_failed');
    renderTab();
    await waitFor(() => {
      expect(mockToastError).toHaveBeenCalledWith(enSettings.composio.toast_connect_failed);
    });
    expect(mockReplace).toHaveBeenCalledWith('/acme/settings?tab=integrations');
  });

  it('fires the success callback exactly once under StrictMode double-invoke', async () => {
    searchParamsRef.current = new URLSearchParams('tab=integrations&connected=notion');
    renderTabStrict();
    await waitFor(() => {
      expect(mockToastSuccess).toHaveBeenCalled();
    });
    expect(mockToastSuccess).toHaveBeenCalledTimes(1);
    expect(mockInvalidate).toHaveBeenCalledTimes(1);
  });
});
