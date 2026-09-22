import type { ReactNode } from 'react';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { I18nProvider } from '@goosar/core/i18n/react';
import { configStore } from '@goosar/core/config';
import enCommon from '../locales/en/common.json';
import enWorkspace from '../locales/en/workspace.json';
import { CreateWorkspaceForm } from './create-workspace-form';

const TEST_RESOURCES = {
  en: { common: enCommon, workspace: enWorkspace },
};

const mockMutate = vi.fn();
vi.mock('@goosar/core/workspace/mutations', () => ({
  useCreateWorkspace: () => ({ mutate: mockMutate, isPending: false }),
}));

const mockListWorkspaceTemplates = vi.fn<() => Promise<unknown[]>>(async () => []);
vi.mock('@goosar/core/api', () => ({
  api: {
    listWorkspaceTemplates: () => mockListWorkspaceTemplates(),
  },
}));

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function renderForm(onSuccess = vi.fn()) {
  const qc = new QueryClient();
  return render(
    <QueryClientProvider client={qc}>
      <CreateWorkspaceForm onSuccess={onSuccess} />
    </QueryClientProvider>,
    { wrapper: I18nWrapper },
  );
}

describe('CreateWorkspaceForm', () => {
  beforeEach(() => {
    mockMutate.mockReset();
    mockListWorkspaceTemplates.mockReset();
    mockListWorkspaceTemplates.mockResolvedValue([]);
    configStore.setState({ daemonAppUrl: '' });
  });

  it('shows the brand host as the URL prefix when no app URL is configured', () => {
    renderForm();
    expect(screen.getByText('goosar.ru/')).toBeInTheDocument();
  });

  it('shows the deployment host as the URL prefix for self-hosted instances', () => {
    configStore.setState({ daemonAppUrl: 'https://goosar.example.com' });
    renderForm();
    expect(screen.getByText('goosar.example.com/')).toBeInTheDocument();
    expect(screen.queryByText('goosar.ru/')).not.toBeInTheDocument();
  });

  it('auto-generates slug from name until user edits slug', () => {
    renderForm();
    fireEvent.change(screen.getByLabelText(/workspace name/i), {
      target: { value: 'Acme Corp' },
    });
    expect(screen.getByDisplayValue('acme-corp')).toBeInTheDocument();
  });

  it('stops auto-generating slug once user edits slug directly', () => {
    renderForm();
    fireEvent.change(screen.getByLabelText(/workspace url/i), {
      target: { value: 'custom' },
    });
    fireEvent.change(screen.getByLabelText(/workspace name/i), {
      target: { value: 'Different Name' },
    });
    expect(screen.getByDisplayValue('custom')).toBeInTheDocument();
  });

  it('calls onSuccess with the created workspace', async () => {
    const onSuccess = vi.fn();
    mockMutate.mockImplementation((_args, opts) => {
      opts?.onSuccess?.({ id: 'ws-1', slug: 'acme', name: 'Acme' });
    });
    renderForm(onSuccess);
    fireEvent.change(screen.getByLabelText(/workspace name/i), {
      target: { value: 'Acme' },
    });
    fireEvent.click(screen.getByRole('button', { name: /create workspace/i }));
    await waitFor(() =>
      expect(onSuccess).toHaveBeenCalledWith(expect.objectContaining({ slug: 'acme' })),
    );
  });

  it('shows slug-conflict error inline on 409', async () => {
    mockMutate.mockImplementation((_args, opts) => {
      opts?.onError?.({ status: 409 });
    });
    renderForm();
    fireEvent.change(screen.getByLabelText(/workspace name/i), {
      target: { value: 'Taken' },
    });
    fireEvent.click(screen.getByRole('button', { name: /create workspace/i }));
    await waitFor(() => expect(screen.getByText(/already taken/i)).toBeInTheDocument());
  });

  it('disables submit when slug has invalid format', () => {
    renderForm();
    fireEvent.change(screen.getByLabelText(/workspace name/i), {
      target: { value: 'Valid Name' },
    });
    fireEvent.change(screen.getByLabelText(/workspace url/i), {
      target: { value: 'Invalid Slug!' },
    });
    expect(screen.getByRole('button', { name: /create workspace/i })).toBeDisabled();
  });

  it('disables submit when slug is reserved', () => {
    renderForm();
    fireEvent.change(screen.getByLabelText(/workspace name/i), {
      target: { value: 'Valid Name' },
    });
    fireEvent.change(screen.getByLabelText(/workspace url/i), {
      target: { value: 'admin' },
    });
    expect(screen.getByRole('button', { name: /create workspace/i })).toBeDisabled();
    expect(screen.getByText(/reserved and cannot be used/i)).toBeInTheDocument();
  });

  it('hides the template section when the catalog is empty', async () => {
    renderForm();
    await waitFor(() => expect(mockListWorkspaceTemplates).toHaveBeenCalled());
    expect(screen.queryByText(/no template/i)).not.toBeInTheDocument();
  });

  it("offers 'no template' first and submits without template_key by default", async () => {
    mockListWorkspaceTemplates.mockResolvedValue([
      { key: 'hr', name: 'HR', description: 'People workflows' },
      { key: 'sales', name: 'Sales', description: 'CRM and mail' },
    ]);
    renderForm();
    await screen.findByText('HR');

    const options = screen.getAllByRole('radio');
    expect(options.length).toBe(3);
    expect(screen.getByText(/no template/i).compareDocumentPosition(screen.getByText('HR'))).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    );

    fireEvent.change(screen.getByLabelText(/workspace name/i), {
      target: { value: 'Acme' },
    });
    fireEvent.click(screen.getByRole('button', { name: /create workspace/i }));
    await waitFor(() => expect(mockMutate).toHaveBeenCalled());
    const payload = mockMutate.mock.calls[0]?.[0] as Record<string, unknown>;
    expect(payload).toEqual({ name: 'Acme', slug: 'acme' });
    expect('template_key' in payload).toBe(false);
  });

  it('submits the selected template key', async () => {
    mockListWorkspaceTemplates.mockResolvedValue([
      { key: 'hr', name: 'HR', description: 'People workflows' },
      { key: 'sales', name: 'Sales', description: 'CRM and mail' },
    ]);
    renderForm();
    await screen.findByText('Sales');

    fireEvent.click(screen.getByText('Sales'));
    fireEvent.change(screen.getByLabelText(/workspace name/i), {
      target: { value: 'Acme Sales' },
    });
    fireEvent.click(screen.getByRole('button', { name: /create workspace/i }));
    await waitFor(() => expect(mockMutate).toHaveBeenCalled());
    expect(mockMutate.mock.calls[0]?.[0]).toEqual({
      name: 'Acme Sales',
      slug: 'acme-sales',
      template_key: 'sales',
    });
  });
});
