import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { RunConfirmModal } from './run-confirm';

const cache = {
  agents: [{ id: 'agent-1', runtime_id: 'runtime-1' }] as Array<{ id: string; runtime_id: string }>,
  runtimes: [{ id: 'runtime-1', metadata: { cli_version: '0.4.0' } }] as Array<{
    id: string;
    metadata: Record<string, unknown>;
  }>,
  squads: [{ id: 'squad-1', leader_id: 'agent-1' }] as Array<{ id: string; leader_id: string }>,
};
vi.mock('@tanstack/react-query', () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => {
    if (queryKey[0] === 'runtimes') return { data: cache.runtimes };
    if (queryKey[0] === 'workspaces' && queryKey[2] === 'agents') return { data: cache.agents };
    if (queryKey[0] === 'workspaces' && queryKey[2] === 'squads') return { data: cache.squads };
    return { data: [] };
  },
}));
vi.mock('@goosar/core/hooks', () => ({ useWorkspaceId: () => 'ws-test' }));
vi.mock('@goosar/core/workspace/queries', () => ({
  agentListOptions: (wsId: string) => ({ queryKey: ['workspaces', wsId, 'agents'] }),
  squadListOptions: (wsId: string) => ({ queryKey: ['workspaces', wsId, 'squads'] }),
}));
vi.mock('@goosar/core/runtimes', () => ({
  runtimeListOptions: (wsId: string) => ({ queryKey: ['runtimes', wsId, 'list'] }),
  readRuntimeCliVersion: (m?: { cli_version?: unknown }) =>
    typeof m?.cli_version === 'string' ? m.cli_version : '',
  handoffSupported: (v?: string | null) => {
    const m = /(\d+)\.(\d+)\.(\d+)/.exec((v ?? '').trim());
    if (!m) return false;
    return Number(m[1]) * 1e6 + Number(m[2]) * 1e3 + Number(m[3]) >= 3028; 
  },
}));

const mockUpdate = vi.fn().mockResolvedValue({ id: 'issue-1' });
const mockBatch = vi.fn().mockResolvedValue({ updated: 2 });
vi.mock('@goosar/core/issues/mutations', () => ({
  useUpdateIssue: () => ({ mutateAsync: mockUpdate }),
  useBatchUpdateIssues: () => ({ mutateAsync: mockBatch }),
}));

vi.mock('@goosar/core/workspace/hooks', () => ({
  useActorName: () => ({ getActorName: () => 'Walt' }),
}));

vi.mock('../i18n', () => ({
  useT: () => ({
    t: (
      sel: (x: Record<string, Record<string, string>>) => string,
      vars?: Record<string, unknown>,
    ) => {
      const labels = {
        run_confirm: {
          title_assign: 'Confirm assignment?',
          assign_single: 'assign to {{name}}',
          assign_batch: 'assign {{count}} to {{name}}',
          note_label: 'Handoff note',
          note_placeholder: 'scope...',
          note_unsupported: 'runtime too old',
          confirm_assign: 'Confirm assignment',
          dont_start: "Don't start yet",
          toast_failed: 'failed',
        },
      };
      return sel(labels).replace(/\{\{(\w+)\}\}/g, (_m, k) => String(vars?.[k] ?? ''));
    },
  }),
}));

vi.mock('@goosar/ui/components/ui/dialog', () => ({
  Dialog: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogFooter: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogDescription: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));
vi.mock('@goosar/ui/components/ui/button', () => ({
  Button: ({ children, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button {...props}>{children}</button>
  ),
}));
vi.mock('@goosar/ui/components/ui/textarea', () => ({
  Textarea: (props: React.TextareaHTMLAttributes<HTMLTextAreaElement>) => <textarea {...props} />,
}));
vi.mock('@goosar/ui/components/ui/spinner', () => ({
  Spinner: () => <span data-testid="spinner" />,
}));
const mockToast = vi.hoisted(() => ({ error: vi.fn(), success: vi.fn() }));
vi.mock('sonner', () => ({ toast: mockToast }));

beforeEach(() => {
  mockUpdate.mockClear().mockResolvedValue({ id: 'issue-1' });
  mockBatch.mockClear().mockResolvedValue({ updated: 2 });
  mockToast.error.mockClear();
  mockToast.success.mockClear();
  cache.agents = [{ id: 'agent-1', runtime_id: 'runtime-1' }];
  cache.runtimes = [{ id: 'runtime-1', metadata: { cli_version: '0.4.0' } }];
  cache.squads = [{ id: 'squad-1', leader_id: 'agent-1' }];
});

const single = {
  issueIds: ['issue-1'],
  mode: 'assign' as const,
  assigneeType: 'agent' as const,
  assigneeId: 'agent-1',
};

describe('RunConfirmModal', () => {
  it('is fully operable on the first frame — no preview request, no spinner', () => {
    const { container } = render(<RunConfirmModal onClose={vi.fn()} data={single} />);
    expect(screen.queryByTestId('spinner')).not.toBeInTheDocument();
    expect(screen.getByPlaceholderText('scope...')).not.toBeDisabled();
    expect(screen.getByText('Confirm assignment')).not.toBeDisabled();
    expect(container.textContent).toContain('assign to Walt');
  });

  it('single assign sends the assignee change with the handoff note', async () => {
    render(<RunConfirmModal onClose={vi.fn()} data={single} />);
    fireEvent.change(screen.getByPlaceholderText('scope...'), { target: { value: 'only login' } });
    fireEvent.click(screen.getByText('Confirm assignment'));
    await waitFor(() => expect(mockUpdate).toHaveBeenCalledTimes(1));
    expect(mockUpdate).toHaveBeenCalledWith({
      id: 'issue-1',
      assignee_type: 'agent',
      assignee_id: 'agent-1',
      handoff_note: 'only login',
    });
    expect(mockBatch).not.toHaveBeenCalled();
  });

  it('completes silently on success — closes with no result toast', async () => {
    const onClose = vi.fn();
    render(<RunConfirmModal onClose={onClose} data={single} />);
    fireEvent.click(screen.getByText('Confirm assignment'));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(mockToast.success).not.toHaveBeenCalled();
    expect(mockToast.error).not.toHaveBeenCalled();
  });

  it("'暂不开始' sends suppress_run and no handoff note", async () => {
    render(<RunConfirmModal onClose={vi.fn()} data={single} />);
    fireEvent.change(screen.getByPlaceholderText('scope...'), { target: { value: 'ignored' } });
    fireEvent.click(screen.getByText("Don't start yet"));
    await waitFor(() => expect(mockUpdate).toHaveBeenCalledTimes(1));
    const payload = mockUpdate.mock.calls[0]![0];
    expect(payload.suppress_run).toBe(true);
    expect(payload.handoff_note).toBeUndefined();
    expect(mockToast.success).not.toHaveBeenCalled();
  });

  it("disables the note box when the agent's runtime is too old", () => {
    cache.runtimes = [{ id: 'runtime-1', metadata: { cli_version: '0.2.21' } }];
    render(<RunConfirmModal onClose={vi.fn()} data={single} />);
    expect(screen.getByPlaceholderText('scope...')).toBeDisabled();
    expect(screen.getByText('runtime too old')).toBeInTheDocument();
  });

  it("resolves a squad's verdict through its leader's runtime, locally", () => {
    cache.runtimes = [{ id: 'runtime-1', metadata: { cli_version: '0.2.21' } }];
    render(
      <RunConfirmModal
        onClose={vi.fn()}
        data={{ ...single, assigneeType: 'squad', assigneeId: 'squad-1' }}
      />,
    );
    expect(screen.getByPlaceholderText('scope...')).toBeDisabled();
    expect(screen.getByText('runtime too old')).toBeInTheDocument();
  });

  it("leaves the note box enabled when the target runtime can't be resolved", () => {
    cache.agents = [];
    render(<RunConfirmModal onClose={vi.fn()} data={single} />);
    expect(screen.getByPlaceholderText('scope...')).not.toBeDisabled();
    expect(screen.queryByText('runtime too old')).not.toBeInTheDocument();
  });

  it('batch assign (N ids) applies via batchUpdate', async () => {
    const { container } = render(
      <RunConfirmModal onClose={vi.fn()} data={{ ...single, issueIds: ['i1', 'i2'] }} />,
    );
    expect(container.textContent).toContain('assign 2 to Walt');
    fireEvent.click(screen.getByText('Confirm assignment'));
    await waitFor(() => expect(mockBatch).toHaveBeenCalledTimes(1));
    expect(mockBatch).toHaveBeenCalledWith({
      ids: ['i1', 'i2'],
      updates: { assignee_type: 'agent', assignee_id: 'agent-1' },
    });
    expect(mockUpdate).not.toHaveBeenCalled();
    expect(mockToast.success).not.toHaveBeenCalled();
  });

  it('keeps the dialog open and surfaces the error when the write fails', async () => {
    const onClose = vi.fn();
    mockUpdate.mockRejectedValue(new Error('boom'));
    render(<RunConfirmModal onClose={onClose} data={single} />);
    fireEvent.click(screen.getByText('Confirm assignment'));
    await waitFor(() => expect(mockToast.error).toHaveBeenCalledWith('boom'));
    expect(onClose).not.toHaveBeenCalled();
    expect(mockToast.success).not.toHaveBeenCalled();
  });
});
