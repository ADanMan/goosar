import { forwardRef, useImperativeHandle, useRef, useState, type ReactNode } from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../locales/en/common.json';
import enModals from '../locales/en/modals.json';
import enEditor from '../locales/en/editor.json';

const TEST_RESOURCES = {
  en: { common: enCommon, modals: enModals, editor: enEditor },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

const mockPush = vi.hoisted(() => vi.fn());
const mockCreateIssue = vi.hoisted(() => vi.fn());
const mockAttachLabel = vi.hoisted(() => vi.fn());
const mockListProperties = vi.hoisted(() => vi.fn());
const mockSetIssueProperty = vi.hoisted(() => vi.fn());
const mockSetShared = vi.hoisted(() => vi.fn());
const mockSetManual = vi.hoisted(() => vi.fn());
const mockSetAgent = vi.hoisted(() => vi.fn());
const mockSetActiveMode = vi.hoisted(() => vi.fn());
const mockClearDraft = vi.hoisted(() => vi.fn());
const mockSetLastAssignee = vi.hoisted(() => vi.fn());
const mockSetKeepOpen = vi.hoisted(() => vi.fn());
const mockToastCustom = vi.hoisted(() => vi.fn());
const mockToastDismiss = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());
const mockApiUploadFile = vi.hoisted(() => vi.fn());

type DraftAttachment = {
  id: string;
  workspace_id: string;
  issue_id: string | null;
  comment_id: string | null;
  chat_session_id: string | null;
  chat_message_id: string | null;
  uploader_type: string;
  uploader_id: string;
  filename: string;
  url: string;
  download_url: string;
  markdown_url: string;
  content_type: string;
  size_bytes: number;
  created_at: string;
};

type DraftUploadEntry = {
  clientUploadId: string;
  status: 'uploading' | 'uploaded' | 'failed' | 'interrupted';
  filename: string;
  size: number;
  contentType?: string;
  attachment?: DraftAttachment;
  error?: string;
};

const emptyIssueDraft = () => ({
  shared: {
    projectId: undefined as string | undefined,
    priority: 'none' as 'none' | 'low' | 'medium' | 'high' | 'urgent',
    dueDate: null as string | null,
    attachments: [] as DraftUploadEntry[],
  },
  manual: {
    title: '',
    description: '',
    status: 'todo' as const,
    startDate: null as string | null,
    assigneeType: undefined as 'agent' | 'squad' | 'member' | undefined,
    assigneeId: undefined as string | undefined,
    labelIds: [] as string[],
    propertyValues: {} as Record<string, string | number | boolean | string[]>,
  },
  agent: {
    prompt: '',
    actorType: undefined as 'agent' | 'squad' | undefined,
    actorId: undefined as string | undefined,
  },
  activeMode: 'manual' as 'manual' | 'agent',
});

const mockDraftStore = {
  draft: emptyIssueDraft(),
  lastAssigneeType: undefined as 'agent' | 'squad' | 'member' | undefined,
  lastAssigneeId: undefined as string | undefined,
  setShared: mockSetShared,
  setManual: mockSetManual,
  setAgent: mockSetAgent,
  setActiveMode: mockSetActiveMode,
  clearDraft: mockClearDraft,
  setLastAssignee: mockSetLastAssignee,
  hasDraft: () => false,
};

const mockQuickCreateStore = {
  keepOpen: false,
  setKeepOpen: mockSetKeepOpen,
};

type ManualCreateField =
  'status' | 'priority' | 'assignee' | 'labels' | 'project' | 'due_date' | 'start_date';

const DEFAULT_MANUAL_FIELDS: ManualCreateField[] = [
  'status',
  'priority',
  'assignee',
  'labels',
  'project',
];

const mockCreateSettingsStore = {
  manualCreateFields: DEFAULT_MANUAL_FIELDS as ManualCreateField[],
};

let mockUploadIdSeq = 0;

vi.mock('../navigation', () => ({
  useNavigation: () => ({ push: mockPush }),
}));

vi.mock('@goosar/core/paths', () => ({
  useCurrentWorkspace: () => ({ name: 'Test Workspace' }),
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/ws-test/issues/${id}`,
    settings: () => '/ws-test/settings',
  }),
}));

vi.mock('@goosar/core/hooks', () => ({
  useWorkspaceId: () => 'ws-test',
}));

vi.mock('@goosar/core/issues/queries', () => ({
  issueDetailOptions: (wsId: string, id: string) => ({
    queryKey: ['issues', wsId, 'detail', id],
    queryFn: () => Promise.resolve(null),
  }),
  childIssuesOptions: (wsId: string, id: string) => ({
    queryKey: ['issues', wsId, 'children', id],
    queryFn: () => Promise.resolve([]),
  }),
}));

vi.mock('../issues/hooks/use-issue-trigger-preview', () => ({
  useIssueTriggerPreview: () => ({
    triggers: [],
    totalCount: 0,
    isLoading: false,
    handoffSupported: false,
  }),
}));

vi.mock('@goosar/core/workspace/hooks', () => ({
  useActorName: () => ({ getActorName: () => 'Agent' }),
}));

vi.mock('../common/actor-avatar', () => ({
  ActorAvatar: () => null,
}));

vi.mock('@goosar/core/issues/stores/draft-store', () => ({
  useIssueDraftStore: Object.assign(
    (selector?: (state: typeof mockDraftStore) => unknown) =>
      selector ? selector(mockDraftStore) : mockDraftStore,
    { getState: () => mockDraftStore },
  ),
}));

vi.mock('@goosar/core/issues/stores/quick-create-store', () => ({
  useQuickCreateStore: (selector?: (state: typeof mockQuickCreateStore) => unknown) =>
    selector ? selector(mockQuickCreateStore) : mockQuickCreateStore,
}));

vi.mock('@goosar/core/issues/stores/issue-create-settings-store', () => ({
  useIssueCreateSettingsStore: (selector?: (state: typeof mockCreateSettingsStore) => unknown) =>
    selector ? selector(mockCreateSettingsStore) : mockCreateSettingsStore,
}));

vi.mock('@goosar/core/issues/mutations', () => ({
  useCreateIssue: () => ({ mutateAsync: mockCreateIssue }),
  useUpdateIssue: () => ({ mutate: vi.fn() }),
}));

vi.mock('@goosar/core/labels', () => ({
  useAttachLabelToIssue: () => ({ mutateAsync: mockAttachLabel }),
}));

vi.mock('@goosar/core/properties', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@goosar/core/properties')>();
  return {
    ...actual,
    useSetIssueProperty: () => ({
      mutateAsync: ({
        issueId,
        propertyId,
        value,
      }: {
        issueId: string;
        propertyId: string;
        value: string | number | boolean | string[];
      }) => mockSetIssueProperty(issueId, propertyId, value),
    }),
  };
});

const { ApiError } = vi.hoisted(() => {
  class ApiErrorImpl extends Error {
    readonly status: number;
    readonly statusText: string;
    readonly body?: unknown;
    constructor(message: string, status: number, statusText: string, body?: unknown) {
      super(message);
      this.name = 'ApiError';
      this.status = status;
      this.statusText = statusText;
      this.body = body;
    }
  }
  return { ApiError: ApiErrorImpl };
});

vi.mock('@goosar/core/api', async () => {
  const { parseWithFallback } =
    await vi.importActual<typeof import('@goosar/core/api/schema')>('@goosar/core/api/schema');
  const { DuplicateIssueErrorBodySchema } = await vi.importActual<
    typeof import('@goosar/core/api/schemas')
  >('@goosar/core/api/schemas');
  return {
    api: {
      listProperties: mockListProperties,
      setIssueProperty: mockSetIssueProperty,
      uploadFile: mockApiUploadFile,
    },
    ApiError,
    parseWithFallback,
    DuplicateIssueErrorBodySchema,
  };
});

vi.mock('../editor', async () => {
  const uploadGate = await vi.importActual<typeof import('../editor/use-upload-gate')>(
    '../editor/use-upload-gate',
  );
  const composer = await vi.importActual<typeof import('../editor/use-composer-submit')>(
    '../editor/use-composer-submit',
  );
  const ContentEditor = forwardRef(
    (
      {
        defaultValue,
        onUpdate,
        onSubmit,
        onUploadFile,
        onUploadingChange,
        placeholder,
        attachments,
      }: any,
      ref: any,
    ) => {
      const valueRef = useRef(defaultValue || '');
      const [value, setValue] = useState(defaultValue || '');
      const inFlightRef = useRef(0);
      useImperativeHandle(ref, () => ({
        getMarkdown: () => valueRef.current,
        clearContent: () => {
          valueRef.current = '';
          setValue('');
        },
        uploadFile: async (file: File) => {
          inFlightRef.current += 1;
          if (inFlightRef.current === 1) onUploadingChange?.(true);
          try {
            return await onUploadFile?.(file, `mock-upload-${++mockUploadIdSeq}`);
          } finally {
            inFlightRef.current -= 1;
            if (inFlightRef.current === 0) onUploadingChange?.(false);
          }
        },
        hasActiveUploads: () => inFlightRef.current > 0,
        insertUploadPlaceholder: () => true,
        settleUploadPlaceholder: () => false,
      }));
      return (
        <>
          <textarea
            value={value}
            placeholder={placeholder}
            data-attachments-count={attachments?.length ?? 0}
            onChange={(e) => {
              valueRef.current = e.target.value;
              setValue(e.target.value);
              onUpdate?.(e.target.value);
            }}
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') onSubmit?.();
            }}
          />
        </>
      );
    },
  );
  ContentEditor.displayName = 'ContentEditor';

  const TitleEditor = forwardRef(
    ({ defaultValue, placeholder, onChange, onSubmit, onSubmitShortcut }: any, ref: any) => {
      const [value, setValue] = useState(defaultValue || '');
      const inputRef = useRef<HTMLInputElement>(null);
      useImperativeHandle(ref, () => ({
        getText: () => value,
        focus: () => inputRef.current?.focus(),
        focusAtCoords: () => inputRef.current?.focus(),
      }));
      return (
        <input
          ref={inputRef}
          value={value}
          placeholder={placeholder}
          onChange={(e) => {
            setValue(e.target.value);
            onChange?.(e.target.value);
          }}
          onKeyDown={(e) => {
            if (e.key !== 'Enter') return;
            if (e.metaKey || e.ctrlKey) onSubmitShortcut?.();
            else onSubmit?.();
          }}
        />
      );
    },
  );
  TitleEditor.displayName = 'TitleEditor';

  return {
    ...uploadGate,
    ...composer,
    useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
    FileDropOverlay: () => null,
    ContentEditor,
    TitleEditor,
  };
});

vi.mock('../issues/components', () => ({
  StatusIcon: ({ status }: { status: string }) => <span data-testid="status-icon">{status}</span>,
  StatusPicker: () => <div data-testid="status-picker" />,
  PriorityPicker: () => <div data-testid="priority-picker" />,
  StagePicker: () => <div data-testid="stage-picker" />,
  AssigneePicker: () => <div data-testid="assignee-picker" />,
  StartDatePicker: ({
    open,
    onOpenChange,
  }: {
    open?: boolean;
    onOpenChange?: (v: boolean) => void;
  }) => (
    <div
      data-testid="start-date-picker"
      data-open={open ? 'true' : 'false'}
      onClick={() => onOpenChange?.(false)}
    />
  ),
  DueDatePicker: ({
    open,
    onOpenChange,
  }: {
    open?: boolean;
    onOpenChange?: (v: boolean) => void;
  }) => (
    <div
      data-testid="due-date-picker"
      data-open={open ? 'true' : 'false'}
      onClick={() => onOpenChange?.(false)}
    />
  ),
  LabelPicker: ({
    open,
    onOpenChange,
  }: {
    open?: boolean;
    onOpenChange?: (v: boolean) => void;
  }) => (
    <div
      data-testid="label-picker"
      data-open={open ? 'true' : 'false'}
      onClick={() => onOpenChange?.(false)}
    />
  ),
}));

vi.mock('../issues/components/pickers/custom-property-picker', () => ({
  CustomPropertyValueInput: ({ property, onChange }: any) => (
    <button
      type="button"
      aria-label={`Edit ${property.name}`}
      onClick={() => onChange('option-enterprise')}
    >
      {property.name}
    </button>
  ),
  CustomPropertyValueDisplay: ({ value }: any) => <span>{String(value)}</span>,
}));

vi.mock('../projects/components/project-picker', () => ({
  ProjectPicker: ({ projectId, onUpdate }: any) => (
    <button
      type="button"
      data-testid="project-picker"
      data-project-id={projectId ?? 'none'}
      onClick={() => onUpdate({ project_id: 'proj-1' })}
    >
      Project {projectId ?? 'none'}
    </button>
  ),
}));

vi.mock('@goosar/ui/components/ui/dialog', () => ({
  Dialog: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="dialog-root">{children}</div>
  ),
  DialogContent: ({ children, className }: { children: React.ReactNode; className?: string }) => (
    <div className={className}>{children}</div>
  ),
  DialogTitle: ({ children, className }: { children: React.ReactNode; className?: string }) => (
    <div className={className}>{children}</div>
  ),
}));

vi.mock('@goosar/ui/components/ui/dropdown-menu', () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuItem: ({
    children,
    onClick,
  }: {
    children: React.ReactNode;
    onClick?: () => void;
  }) => (
    <button type="button" onClick={onClick}>
      {children}
    </button>
  ),
  DropdownMenuSeparator: () => null,
  DropdownMenuSub: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuSubTrigger: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuSubContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

vi.mock('./issue-picker-modal', () => ({
  IssuePickerModal: () => null,
}));

vi.mock('@goosar/ui/components/ui/tooltip', () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

vi.mock('@goosar/ui/components/ui/button', () => ({
  Button: ({
    children,
    disabled,
    onClick,
    type = 'button',
    ...rest
  }: {
    children: React.ReactNode;
    disabled?: boolean;
    onClick?: () => void;
    type?: 'button' | 'submit' | 'reset';
    [key: string]: unknown;
  }) => (
    <button type={type} disabled={disabled} onClick={onClick} {...rest}>
      {children}
    </button>
  ),
}));

vi.mock('@goosar/ui/components/ui/switch', () => ({
  Switch: ({
    checked,
    onCheckedChange,
  }: {
    checked: boolean;
    onCheckedChange: (v: boolean) => void;
  }) => (
    <input
      aria-label="Create another"
      type="checkbox"
      checked={checked}
      onChange={(e) => onCheckedChange(e.target.checked)}
    />
  ),
}));

vi.mock('@goosar/ui/components/common/file-upload-button', () => ({
  FileUploadButton: ({ onSelect }: { onSelect: (file: File) => void }) => (
    <button type="button" onClick={() => onSelect(new File(['test'], 'test.txt'))}>
      Upload file
    </button>
  ),
}));

vi.mock('@goosar/ui/lib/utils', () => ({
  cn: (...values: Array<string | false | null | undefined>) => values.filter(Boolean).join(' '),
}));

vi.mock('sonner', () => ({
  toast: {
    custom: mockToastCustom,
    dismiss: mockToastDismiss,
    error: mockToastError,
  },
}));

import { CreateIssueModal, ManualCreatePanel } from './create-issue';

function renderModal(element: React.ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <I18nWrapper>
      <QueryClientProvider client={qc}>{element}</QueryClientProvider>
    </I18nWrapper>,
  );
}

describe('CreateIssueModal', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockQuickCreateStore.keepOpen = false;
    mockCreateSettingsStore.manualCreateFields = DEFAULT_MANUAL_FIELDS;
    mockSetKeepOpen.mockImplementation((v: boolean) => {
      mockQuickCreateStore.keepOpen = v;
    });
    mockDraftStore.draft = emptyIssueDraft();
    mockSetShared.mockImplementation((patch: Partial<typeof mockDraftStore.draft.shared>) => {
      mockDraftStore.draft.shared = { ...mockDraftStore.draft.shared, ...patch };
    });
    mockSetManual.mockImplementation((patch: Partial<typeof mockDraftStore.draft.manual>) => {
      mockDraftStore.draft.manual = { ...mockDraftStore.draft.manual, ...patch };
    });
    mockSetAgent.mockImplementation((patch: Partial<typeof mockDraftStore.draft.agent>) => {
      mockDraftStore.draft.agent = { ...mockDraftStore.draft.agent, ...patch };
    });
    mockClearDraft.mockImplementation(() => {
      const next = emptyIssueDraft();
      next.manual.assigneeType = mockDraftStore.lastAssigneeType;
      next.manual.assigneeId = mockDraftStore.lastAssigneeId;
      mockDraftStore.draft = next;
    });
    mockApiUploadFile.mockResolvedValue({
      id: '11111111-2222-3333-4444-555555555555',
      workspace_id: 'ws-test',
      issue_id: null,
      comment_id: null,
      chat_session_id: null,
      chat_message_id: null,
      uploader_type: 'member',
      uploader_id: 'user-1',
      filename: 'shot.png',
      url: 'https://cdn.example.test/shot.png',
      download_url: 'https://cdn.example.test/shot.png?Signature=fresh',
      markdown_url:
        'https://api.goosar.ru/api/attachments/11111111-2222-3333-4444-555555555555/download',
      content_type: 'image/png',
      size_bytes: 123,
      created_at: '2026-06-12T00:00:00Z',
    });
    mockCreateIssue.mockResolvedValue({
      id: 'issue-123',
      identifier: 'TES-123',
      title: 'Ship create issue regression coverage',
      status: 'todo',
      labels: [],
    });
    mockAttachLabel.mockResolvedValue({ labels: [] });
    mockListProperties.mockResolvedValue({
      properties: [
        {
          id: 'property-tier',
          workspace_id: 'ws-test',
          name: 'Customer tier',
          type: 'select',
          config: {
            options: [{ id: 'option-enterprise', name: 'Enterprise', color: '#3b82f6' }],
          },
          position: 0,
          archived: false,
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
      ],
      total: 1,
    });
    mockSetIssueProperty.mockResolvedValue({
      properties: { 'property-tier': 'option-enterprise' },
    });
  });

  it('shows success feedback with a direct path to the new issue', async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();

    renderModal(<CreateIssueModal onClose={onClose} />);

    fireEvent.change(screen.getByPlaceholderText('Task title'), {
      target: { value: '  Ship create issue regression coverage  ' },
    });
    await user.click(screen.getByRole('button', { name: 'Create Task' }));

    await waitFor(() => {
      expect(mockCreateIssue).toHaveBeenCalledWith({
        title: 'Ship create issue regression coverage',
        description: undefined,
        status: 'todo',
        priority: 'none',
        assignee_type: undefined,
        assignee_id: undefined,
        start_date: undefined,
        due_date: undefined,
        attachment_ids: undefined,
        parent_issue_id: undefined,
        project_id: undefined,
      });
    });

    expect(mockSetLastAssignee).toHaveBeenCalledWith(undefined, undefined);
    expect(mockClearDraft).toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
    expect(mockToastCustom).toHaveBeenCalledTimes(1);

    const renderToast = mockToastCustom.mock.calls[0]?.[0];
    expect(typeof renderToast).toBe('function');

    render(renderToast('toast-1'));

    expect(screen.getByText('Task created')).toBeInTheDocument();
    expect(screen.getByText(/TES-123/)).toBeInTheDocument();
    expect(screen.getByText(/Ship create issue regression coverage/)).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'View task' }));

    expect(mockPush).toHaveBeenCalledWith('/ws-test/issues/issue-123');
    expect(mockToastDismiss).toHaveBeenCalledWith('toast-1');
  });

  it('forwards selected labels in the create payload so they attach in the same transaction', async () => {
    const user = userEvent.setup();
    mockDraftStore.draft.manual.labelIds = [
      'aaaaaaaa-1111-2222-3333-444444444444',
      'bbbbbbbb-1111-2222-3333-444444444444',
    ];

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    fireEvent.change(screen.getByPlaceholderText('Task title'), {
      target: { value: 'Labeled issue' },
    });
    await user.click(screen.getByRole('button', { name: 'Create Task' }));

    await waitFor(() => {
      expect(mockCreateIssue).toHaveBeenCalledWith(
        expect.objectContaining({
          title: 'Labeled issue',
          label_ids: [
            'aaaaaaaa-1111-2222-3333-444444444444',
            'bbbbbbbb-1111-2222-3333-444444444444',
          ],
        }),
      );
    });
    expect(mockAttachLabel).not.toHaveBeenCalled();
  });

  it('falls back to per-label attach when an older backend omits labels from the create response', async () => {
    const user = userEvent.setup();
    mockCreateIssue.mockResolvedValueOnce({
      id: 'issue-123',
      identifier: 'TES-123',
      title: 'Labeled issue',
      status: 'todo',
    });
    mockDraftStore.draft.manual.labelIds = [
      'aaaaaaaa-1111-2222-3333-444444444444',
      'bbbbbbbb-1111-2222-3333-444444444444',
    ];

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    fireEvent.change(screen.getByPlaceholderText('Task title'), {
      target: { value: 'Labeled issue' },
    });
    await user.click(screen.getByRole('button', { name: 'Create Task' }));

    await waitFor(() => {
      expect(mockAttachLabel).toHaveBeenCalledTimes(2);
    });
    expect(mockAttachLabel).toHaveBeenCalledWith({
      issueId: 'issue-123',
      labelId: 'aaaaaaaa-1111-2222-3333-444444444444',
    });
    expect(mockAttachLabel).toHaveBeenCalledWith({
      issueId: 'issue-123',
      labelId: 'bbbbbbbb-1111-2222-3333-444444444444',
    });
  });

  it('keeps manual mode open and clears content when create another is enabled', async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    mockQuickCreateStore.keepOpen = true;

    renderModal(<CreateIssueModal onClose={onClose} />);

    await user.type(screen.getByPlaceholderText('Task title'), 'First follow-up issue');
    await user.type(screen.getByPlaceholderText('Add description...'), 'Description to clear');
    await user.click(screen.getByRole('button', { name: 'Create Task' }));

    await waitFor(() => {
      expect(mockCreateIssue).toHaveBeenCalledWith({
        title: 'First follow-up issue',
        description: 'Description to clear',
        status: 'todo',
        priority: 'none',
        assignee_type: undefined,
        assignee_id: undefined,
        start_date: undefined,
        due_date: undefined,
        attachment_ids: undefined,
        parent_issue_id: undefined,
        project_id: undefined,
      });
    });

    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByPlaceholderText('Task title')).toHaveValue('');
    expect(screen.getByPlaceholderText('Add description...')).toHaveValue('');
    expect(mockSetManual).toHaveBeenCalledWith({
      title: '',
      description: '',
      status: 'todo',
      assigneeType: undefined,
      assigneeId: undefined,
      startDate: null,
      labelIds: [],
      propertyValues: {},
    });
    expect(mockSetShared).toHaveBeenCalledWith({
      priority: 'none',
      projectId: undefined,
      dueDate: null,
      attachments: [],
    });
  });

  it('sets configured custom property values after the issue is created', async () => {
    const user = userEvent.setup();

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    await screen.findByText('Customer tier');
    await user.click(screen.getByText('Customer tier'));
    await user.click(screen.getByRole('button', { name: 'Edit Customer tier' }));
    await user.type(screen.getByPlaceholderText('Task title'), 'Enterprise follow-up');
    await user.click(screen.getByRole('button', { name: 'Create Task' }));

    await waitFor(() => {
      expect(mockSetIssueProperty).toHaveBeenCalledWith(
        'issue-123',
        'property-tier',
        'option-enterprise',
      );
    });
    expect(mockClearDraft).toHaveBeenCalled();
  });

  it('persists manual-mode uploads in the issue draft', async () => {
    const user = userEvent.setup();

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    await user.click(screen.getByRole('button', { name: 'Upload file' }));

    await waitFor(() => {
      const uploads = mockDraftStore.draft.shared.attachments;
      expect(uploads).toHaveLength(1);
      expect(uploads[0]).toMatchObject({ status: 'uploaded', filename: 'shot.png' });
      expect(uploads[0]?.attachment?.id).toBe('11111111-2222-3333-4444-555555555555');
    });
    expect(mockDraftStore.draft.shared.attachments[0]?.attachment?.download_url).toBe('');
  });

  it('reuses draft attachments after reopening manual create so pasted images can render and bind', async () => {
    const user = userEvent.setup();
    const attachment = {
      id: '11111111-2222-3333-4444-555555555555',
      workspace_id: 'ws-test',
      issue_id: null,
      comment_id: null,
      chat_session_id: null,
      chat_message_id: null,
      uploader_type: 'member',
      uploader_id: 'user-1',
      filename: 'shot.png',
      url: 'https://cdn.example.test/shot.png',
      download_url: '',
      markdown_url:
        'https://api.goosar.ru/api/attachments/11111111-2222-3333-4444-555555555555/download',
      content_type: 'image/png',
      size_bytes: 123,
      created_at: '2026-06-12T00:00:00Z',
    };
    mockDraftStore.draft.manual.title = 'Image draft';
    mockDraftStore.draft.manual.description = `![shot.png](${attachment.markdown_url})`;
    mockDraftStore.draft.shared.attachments = [
      {
        clientUploadId: attachment.id,
        status: 'uploaded',
        filename: attachment.filename,
        size: attachment.size_bytes,
        contentType: attachment.content_type,
        attachment,
      },
    ];

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    expect(screen.getByPlaceholderText('Add description...')).toHaveAttribute(
      'data-attachments-count',
      '1',
    );

    await user.click(screen.getByRole('button', { name: 'Create Task' }));

    await waitFor(() => {
      expect(mockCreateIssue).toHaveBeenCalledWith(
        expect.objectContaining({
          description: `![shot.png](${attachment.markdown_url})`,
          attachment_ids: ['11111111-2222-3333-4444-555555555555'],
        }),
      );
    });
  });

  it('prunes draft attachments the reopened description no longer references', async () => {
    const referenced = {
      id: '11111111-2222-3333-4444-555555555555',
      workspace_id: 'ws-test',
      issue_id: null,
      comment_id: null,
      chat_session_id: null,
      chat_message_id: null,
      uploader_type: 'member',
      uploader_id: 'user-1',
      filename: 'kept.png',
      url: 'https://cdn.example.test/kept.png',
      download_url: '',
      markdown_url:
        'https://api.goosar.ru/api/attachments/11111111-2222-3333-4444-555555555555/download',
      content_type: 'image/png',
      size_bytes: 123,
      created_at: '2026-06-12T00:00:00Z',
    };
    const deleted = {
      ...referenced,
      id: '99999999-8888-7777-6666-555555555555',
      filename: 'deleted.png',
      url: 'https://cdn.example.test/deleted.png',
      markdown_url:
        'https://api.goosar.ru/api/attachments/99999999-8888-7777-6666-555555555555/download',
    };
    const wrap = (att: typeof referenced): DraftUploadEntry => ({
      clientUploadId: att.id,
      status: 'uploaded',
      filename: att.filename,
      size: att.size_bytes,
      contentType: att.content_type,
      attachment: att,
    });
    mockDraftStore.draft.manual.title = 'Image draft';
    mockDraftStore.draft.manual.description = `![kept.png](${referenced.markdown_url})`;
    mockDraftStore.draft.shared.attachments = [wrap(referenced), wrap(deleted)];

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    await waitFor(() => {
      expect(mockSetShared).toHaveBeenCalledWith({
        attachments: [expect.objectContaining({ clientUploadId: referenced.id })],
      });
    });
  });

  it('mount prune keeps in-flight placeholders while dropping unreferenced uploaded rows', async () => {
    const orphanRow = {
      id: '99999999-8888-7777-6666-555555555555',
      workspace_id: 'ws-test',
      issue_id: null,
      comment_id: null,
      chat_session_id: null,
      chat_message_id: null,
      uploader_type: 'member',
      uploader_id: 'user-1',
      filename: 'orphan.png',
      url: 'https://cdn.example.test/orphan.png',
      download_url: '',
      markdown_url:
        'https://api.goosar.ru/api/attachments/99999999-8888-7777-6666-555555555555/download',
      content_type: 'image/png',
      size_bytes: 5,
      created_at: '2026-06-12T00:00:00Z',
    };
    mockDraftStore.draft.manual.title = 'Reopened';
    mockDraftStore.draft.shared.attachments = [
      { clientUploadId: 'c-flight', status: 'uploading', filename: 'mid.png', size: 9 },
      {
        clientUploadId: orphanRow.id,
        status: 'uploaded',
        filename: orphanRow.filename,
        size: orphanRow.size_bytes,
        attachment: orphanRow,
      },
    ];

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    await waitFor(() => {
      expect(mockSetShared).toHaveBeenCalledWith({
        attachments: [expect.objectContaining({ clientUploadId: 'c-flight' })],
      });
    });
  });

  it('seeds the agent actor from the picked squad when switching to agent mode', async () => {
    mockDraftStore.draft.manual.assigneeType = 'squad';
    mockDraftStore.draft.manual.assigneeId = 'squad-1';
    const user = userEvent.setup();
    const onSwitchMode = vi.fn();

    renderModal(
      <ManualCreatePanel
        onClose={vi.fn()}
        onSwitchMode={onSwitchMode}
        isExpanded={false}
        setIsExpanded={vi.fn()}
      />,
    );

    await user.type(screen.getByPlaceholderText('Task title'), 'Refactor auth');
    await user.click(screen.getByRole('button', { name: /Switch to Agent/i }));

    expect(onSwitchMode).toHaveBeenCalledTimes(1);
    expect(mockSetAgent).toHaveBeenCalledWith({ prompt: 'Refactor auth' });
    expect(mockSetAgent).toHaveBeenCalledWith({
      actorType: 'squad',
      actorId: 'squad-1',
    });
    expect(onSwitchMode.mock.calls[0]?.[0]).toBeNull();
  });

  it('shows duplicate-issue toast with a working view-existing link', async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    mockCreateIssue.mockRejectedValue(
      new ApiError(
        'An active issue with this title already exists: MUL-7 – Login bug',
        409,
        'Conflict',
        {
          code: 'active_duplicate_issue',
          error: 'An active issue with this title already exists: MUL-7 – Login bug',
          issue: {
            id: 'issue-dup',
            identifier: 'MUL-7',
            title: 'Login bug',
          },
        },
      ),
    );

    renderModal(<CreateIssueModal onClose={onClose} />);
    await user.type(screen.getByPlaceholderText('Task title'), 'Login bug');
    await user.click(screen.getByRole('button', { name: 'Create Task' }));

    await waitFor(() => expect(mockToastCustom).toHaveBeenCalledTimes(1));
    expect(mockToastError).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();

    const renderToast = mockToastCustom.mock.calls[0]?.[0];
    expect(typeof renderToast).toBe('function');
    render(renderToast('toast-dup'));

    expect(screen.getByText('Duplicate task')).toBeInTheDocument();
    expect(screen.getByText(/MUL-7/)).toBeInTheDocument();
    expect(screen.getByText(/Login bug/)).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'View existing task' }));
    expect(mockPush).toHaveBeenCalledWith('/ws-test/issues/issue-dup');
    expect(mockToastDismiss).toHaveBeenCalledWith('toast-dup');
  });

  it('falls back to a normal error toast when a 409 body does not match the duplicate schema', async () => {
    const user = userEvent.setup();
    mockCreateIssue.mockRejectedValue(
      new ApiError('Backend says title is taken', 409, 'Conflict', {
        code: 'renamed_duplicate_marker',
      }),
    );

    renderModal(<CreateIssueModal onClose={vi.fn()} />);
    await user.type(screen.getByPlaceholderText('Task title'), 'Login bug');
    await user.click(screen.getByRole('button', { name: 'Create Task' }));

    await waitFor(() => expect(mockToastError).toHaveBeenCalledTimes(1));
    expect(mockToastError).toHaveBeenCalledWith('Backend says title is taken');
    expect(mockToastCustom).not.toHaveBeenCalled();
  });

  it('surfaces err.message verbatim for non-duplicate errors', async () => {
    const user = userEvent.setup();
    mockCreateIssue.mockRejectedValue(new Error('Server is overloaded, try again'));

    renderModal(<CreateIssueModal onClose={vi.fn()} />);
    await user.type(screen.getByPlaceholderText('Task title'), 'Anything');
    await user.click(screen.getByRole('button', { name: 'Create Task' }));

    await waitFor(() => expect(mockToastError).toHaveBeenCalledTimes(1));
    expect(mockToastError).toHaveBeenCalledWith('Server is overloaded, try again');
  });

  it('falls back to the generic toast when the thrown value is not an Error', async () => {
    const user = userEvent.setup();
    mockCreateIssue.mockRejectedValue('network exploded');

    renderModal(<CreateIssueModal onClose={vi.fn()} />);
    await user.type(screen.getByPlaceholderText('Task title'), 'Anything');
    await user.click(screen.getByRole('button', { name: 'Create Task' }));

    await waitFor(() => expect(mockToastError).toHaveBeenCalledTimes(1));
    expect(mockToastError).toHaveBeenCalledWith('Failed to create task');
  });

  it('commits the picked project to the shared draft when switching to agent mode', async () => {
    const user = userEvent.setup();
    const onSwitchMode = vi.fn();

    renderModal(
      <ManualCreatePanel
        onClose={vi.fn()}
        onSwitchMode={onSwitchMode}
        data={{ project_id: 'proj-1' }}
        isExpanded={false}
        setIsExpanded={vi.fn()}
      />,
    );

    await user.type(screen.getByPlaceholderText('Task title'), 'Refactor auth');

    await user.click(screen.getByRole('button', { name: /Switch to Agent/i }));

    expect(onSwitchMode).toHaveBeenCalledTimes(1);
    expect(mockSetShared).toHaveBeenCalledWith(expect.objectContaining({ projectId: 'proj-1' }));
    expect(mockSetAgent).toHaveBeenCalledWith({ prompt: 'Refactor auth' });
  });

  it('restores an unfinished project selection after manual create remounts', async () => {
    const user = userEvent.setup();

    const firstOpen = renderModal(<CreateIssueModal onClose={vi.fn()} />);
    expect(screen.getByTestId('project-picker')).toHaveAttribute('data-project-id', 'none');

    await user.click(screen.getByTestId('project-picker'));
    expect(mockSetShared).toHaveBeenCalledWith({ projectId: 'proj-1' });

    firstOpen.unmount();
    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    expect(screen.getByTestId('project-picker')).toHaveAttribute('data-project-id', 'proj-1');
  });

  it('forwards parent_issue_id and falls back to seeded identifier when switching to agent mode', async () => {
    const user = userEvent.setup();
    const onSwitchMode = vi.fn();

    renderModal(
      <ManualCreatePanel
        onClose={vi.fn()}
        onSwitchMode={onSwitchMode}
        data={{
          parent_issue_id: 'parent-uuid-1',
          parent_issue_identifier: 'MUL-2534',
        }}
        isExpanded={false}
        setIsExpanded={vi.fn()}
      />,
    );

    await user.type(screen.getByPlaceholderText('Task title'), 'Refactor auth');
    await user.click(screen.getByRole('button', { name: /Switch to Agent/i }));

    expect(onSwitchMode).toHaveBeenCalledTimes(1);
    expect(onSwitchMode.mock.calls[0]?.[0]).toEqual({
      parent_issue_id: 'parent-uuid-1',
      parent_issue_identifier: 'MUL-2534',
    });
    expect(mockSetAgent).toHaveBeenCalledWith({ prompt: 'Refactor auth' });
  });

  it('hides start date behind the overflow menu and reveals it on demand', async () => {
    const user = userEvent.setup();

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    expect(screen.queryByTestId('start-date-picker')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /Set start date/i }));

    const picker = await screen.findByTestId('start-date-picker');
    expect(picker).toHaveAttribute('data-open', 'true');

    await user.click(picker);

    expect(screen.queryByTestId('start-date-picker')).not.toBeInTheDocument();
  });

  it('exposes the label picker on the toolbar and keeps due date in the overflow menu', async () => {
    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    expect(screen.getByTestId('label-picker')).toBeInTheDocument();
    expect(screen.queryByTestId('due-date-picker')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Set due date/i })).toBeInTheDocument();
  });

  it('hides due date behind the overflow menu and reveals it on demand', async () => {
    const user = userEvent.setup();

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    expect(screen.queryByTestId('due-date-picker')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /Set due date/i }));

    const picker = await screen.findByTestId('due-date-picker');
    expect(picker).toHaveAttribute('data-open', 'true');

    await user.click(picker);

    expect(screen.queryByTestId('due-date-picker')).not.toBeInTheDocument();
  });

  it('hides toolbar fields turned off in Settings → Issue and re-reveals them from the overflow', async () => {
    const user = userEvent.setup();
    mockCreateSettingsStore.manualCreateFields = ['status', 'priority', 'assignee', 'project'];

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    expect(screen.queryByTestId('label-picker')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /Set labels/i }));

    const picker = await screen.findByTestId('label-picker');
    expect(picker).toHaveAttribute('data-open', 'true');

    await user.click(picker);

    expect(screen.queryByTestId('label-picker')).not.toBeInTheDocument();
  });

  it('keeps a hidden field on the toolbar while it holds a value', () => {
    mockCreateSettingsStore.manualCreateFields = ['status', 'priority', 'assignee', 'project'];
    mockDraftStore.draft.manual.labelIds = ['label-1'];

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    expect(screen.getByTestId('label-picker')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Set labels/i })).not.toBeInTheDocument();
  });

  it('renders due date inline when enabled in Settings → Issue', () => {
    mockCreateSettingsStore.manualCreateFields = [...DEFAULT_MANUAL_FIELDS, 'due_date'];

    renderModal(<CreateIssueModal onClose={vi.fn()} />);

    expect(screen.getByTestId('due-date-picker')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Set due date/i })).not.toBeInTheDocument();
  });

  it('routes Customize fields to Settings → Issue and closes the dialog', async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();

    renderModal(<CreateIssueModal onClose={onClose} />);

    await user.click(screen.getByRole('button', { name: /Customize fields/i }));

    expect(onClose).toHaveBeenCalled();
    expect(mockPush).toHaveBeenCalledWith('/ws-test/settings?tab=issue');
  });

  it('assist-inits the agent prompt from title and description without clearing the manual draft', async () => {
    const user = userEvent.setup();

    renderModal(
      <ManualCreatePanel
        onClose={vi.fn()}
        onSwitchMode={vi.fn()}
        isExpanded={false}
        setIsExpanded={vi.fn()}
      />,
    );

    await user.type(screen.getByPlaceholderText('Task title'), 'Update');
    await user.type(screen.getByPlaceholderText('Add description...'), 'Some body');

    mockSetManual.mockClear();
    mockSetAgent.mockClear();
    await user.click(screen.getByRole('button', { name: /Switch to Agent/i }));

    expect(mockSetAgent).toHaveBeenCalledWith({ prompt: 'Update\n\nSome body' });
    expect(mockSetManual).not.toHaveBeenCalledWith(expect.objectContaining({ title: '' }));
    expect(mockSetManual).not.toHaveBeenCalledWith(expect.objectContaining({ description: '' }));
  });

  describe('stale-submit draft guard', () => {
    function renderManualPanel(onClose = vi.fn()) {
      return renderModal(
        <ManualCreatePanel
          onClose={onClose}
          onSwitchMode={vi.fn()}
          isExpanded={false}
          setIsExpanded={vi.fn()}
        />,
      );
    }

    async function startPendingCreate() {
      let resolveCreate!: (v: unknown) => void;
      mockCreateIssue.mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolveCreate = resolve;
          }),
      );
      const user = userEvent.setup();
      const onClose = vi.fn();
      const view = renderManualPanel(onClose);
      await user.type(screen.getByPlaceholderText('Task title'), 'Draft A');
      fireEvent.keyDown(screen.getByPlaceholderText('Task title'), {
        key: 'Enter',
        metaKey: true,
      });
      await waitFor(() => expect(mockCreateIssue).toHaveBeenCalled());
      return {
        view,
        onClose,
        finish: () =>
          act(async () => {
            resolveCreate({
              id: 'issue-9',
              identifier: 'TES-9',
              title: 'Draft A',
              status: 'todo',
              labels: [],
            });
            await Promise.resolve();
          }),
      };
    }

    it("typing draft B while draft A's submit is pending survives the success (mounted)", async () => {
      const { onClose, finish } = await startPendingCreate();

      mockDraftStore.draft = {
        ...emptyIssueDraft(),
        manual: { ...emptyIssueDraft().manual, title: 'Draft B' },
      };

      await finish();

      expect(mockClearDraft).not.toHaveBeenCalled();
      expect(onClose).not.toHaveBeenCalled();
      expect(mockDraftStore.draft.manual.title).toBe('Draft B');
    });

    it('an untouched mounted submit still clears and closes', async () => {
      const { onClose, finish } = await startPendingCreate();

      await finish();

      expect(mockClearDraft).toHaveBeenCalled();
      expect(onClose).toHaveBeenCalled();
    });

    it('a late success does NOT clear a draft replaced after the dialog closed', async () => {
      const { view, finish } = await startPendingCreate();

      view.unmount();
      mockDraftStore.draft = {
        ...emptyIssueDraft(),
        manual: { ...emptyIssueDraft().manual, title: 'Draft B' },
      };

      await finish();

      expect(mockClearDraft).not.toHaveBeenCalled();
      expect(mockDraftStore.draft.manual.title).toBe('Draft B');
    });

    it('a late success still clears an untouched draft', async () => {
      const { view, finish } = await startPendingCreate();

      view.unmount();
      await finish();

      expect(mockClearDraft).toHaveBeenCalled();
    });
  });

  describe('upload submit gate', () => {
    function startPendingUpload() {
      let release!: (result: unknown) => void;
      mockApiUploadFile.mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            release = resolve;
          }),
      );
      fireEvent.click(screen.getByRole('button', { name: 'Upload file' }));
      return { release: (result: unknown) => release(result) };
    }

    function renderManual(onSwitchMode = vi.fn()) {
      const view = renderModal(
        <ManualCreatePanel
          onClose={vi.fn()}
          onSwitchMode={onSwitchMode}
          isExpanded={false}
          setIsExpanded={vi.fn()}
        />,
      );
      return { ...view, onSwitchMode };
    }

    it('disables Create and shows Uploading… while an upload is in flight', async () => {
      const user = userEvent.setup();
      renderManual();
      await user.type(screen.getByPlaceholderText('Task title'), 'Has a screenshot');

      const pending = startPendingUpload();

      const createButton = await screen.findByRole('button', { name: 'Uploading…' });
      await waitFor(() => expect(createButton).toBeDisabled());
      expect(createButton).toHaveAttribute('aria-busy', 'true');

      await act(async () => {
        pending.release({
          id: 'att-1',
          url: 'https://cdn/x.png',
          filename: 'x.png',
          size_bytes: 1,
          content_type: 'image/png',
        });
      });
      await waitFor(() =>
        expect(screen.getByRole('button', { name: 'Create Task' })).not.toBeDisabled(),
      );
    });

    it('never submits manual create from plain Enter in the title', async () => {
      const user = userEvent.setup();
      renderManual();
      const title = screen.getByPlaceholderText('Task title');
      await user.type(title, 'Has a screenshot');

      fireEvent.keyDown(title, { key: 'Enter' });
      await Promise.resolve();
      expect(mockCreateIssue).not.toHaveBeenCalled();
    });

    it('blocks the title send chord while an upload is in flight', async () => {
      const user = userEvent.setup();
      renderManual();
      const title = screen.getByPlaceholderText('Task title');
      await user.type(title, 'Has a screenshot');

      startPendingUpload();

      fireEvent.keyDown(title, { key: 'Enter', metaKey: true });
      await Promise.resolve();
      expect(mockCreateIssue).not.toHaveBeenCalled();
    });

    it('blocks Switch to Agent while an upload is in flight', async () => {
      const user = userEvent.setup();
      const onSwitchMode = vi.fn();
      renderManual(onSwitchMode);
      await user.type(screen.getByPlaceholderText('Task title'), 'Has a screenshot');

      startPendingUpload();

      const switchButton = screen.getByRole('button', { name: /Switch to Agent/i });
      await waitFor(() => expect(switchButton).toBeDisabled());
      fireEvent.click(switchButton);
      expect(onSwitchMode).not.toHaveBeenCalled();
    });
  });

  describe('send shortcut', () => {
    function renderManual() {
      return renderModal(
        <ManualCreatePanel
          onClose={vi.fn()}
          onSwitchMode={vi.fn()}
          isExpanded={false}
          setIsExpanded={vi.fn()}
        />,
      );
    }

    it('creates from the send chord in the title', async () => {
      const user = userEvent.setup();
      renderManual();
      const title = screen.getByPlaceholderText('Task title');
      await user.type(title, 'Shortcut from title');

      fireEvent.keyDown(title, { key: 'Enter', metaKey: true });

      await waitFor(() => expect(mockCreateIssue).toHaveBeenCalledTimes(1));
      expect(mockCreateIssue).toHaveBeenCalledWith(
        expect.objectContaining({ title: 'Shortcut from title' }),
      );
    });

    it('creates from the send chord in the description', async () => {
      const user = userEvent.setup();
      renderManual();
      await user.type(screen.getByPlaceholderText('Task title'), 'Shortcut from body');
      const description = screen.getByPlaceholderText('Add description...');
      await user.type(description, 'Body text');

      fireEvent.keyDown(description, { key: 'Enter', ctrlKey: true });

      await waitFor(() => expect(mockCreateIssue).toHaveBeenCalledTimes(1));
      expect(mockCreateIssue).toHaveBeenCalledWith(
        expect.objectContaining({
          title: 'Shortcut from body',
          description: 'Body text',
        }),
      );
    });

    it('leaves plain Enter in the description as a newline, not a create', async () => {
      const user = userEvent.setup();
      renderManual();
      await user.type(screen.getByPlaceholderText('Task title'), 'Still typing');

      fireEvent.keyDown(screen.getByPlaceholderText('Add description...'), { key: 'Enter' });
      await Promise.resolve();
      expect(mockCreateIssue).not.toHaveBeenCalled();
    });

    it('focuses the title instead of silently doing nothing when it is empty', async () => {
      const user = userEvent.setup();
      renderManual();
      const description = screen.getByPlaceholderText('Add description...');
      await user.type(description, 'Body but no title');

      fireEvent.keyDown(description, { key: 'Enter', metaKey: true });

      await Promise.resolve();
      expect(mockCreateIssue).not.toHaveBeenCalled();
      expect(screen.getByPlaceholderText('Task title')).toHaveFocus();
    });

    it('creates once when the chord is pressed twice in the same tick', async () => {
      const user = userEvent.setup();
      let release!: (v: unknown) => void;
      mockCreateIssue.mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            release = resolve;
          }),
      );
      renderManual();
      const title = screen.getByPlaceholderText('Task title');
      await user.type(title, 'Double tap');

      await act(async () => {
        const press = () =>
          title.dispatchEvent(
            new KeyboardEvent('keydown', { key: 'Enter', metaKey: true, bubbles: true }),
          );
        press();
        press();
      });

      await act(async () => {
        release({ id: 'issue-1', identifier: 'MUL-1', title: 'Double tap', status: 'todo' });
      });
      expect(mockCreateIssue).toHaveBeenCalledTimes(1);
    });

    it('renders the send keycaps on Create without renaming the button', async () => {
      const user = userEvent.setup();
      renderManual();

      expect(screen.getByRole('button', { name: 'Create Task' })).toBeInTheDocument();
      expect(document.querySelector("[data-slot='shortcut-keycaps']")).toBeInTheDocument();

      await user.type(screen.getByPlaceholderText('Task title'), 'Now valid');
      expect(screen.getByRole('button', { name: 'Create Task' })).toBeInTheDocument();
      expect(document.querySelector("[data-slot='shortcut-keycaps']")).toBeInTheDocument();
    });

    it('keeps Create focusable via aria-disabled while the title is empty', () => {
      renderManual();
      const createButton = screen.getByRole('button', { name: 'Create Task' });

      expect(createButton).toHaveAttribute('aria-disabled', 'true');
      expect(createButton).not.toBeDisabled();
      createButton.focus();
      expect(createButton).toHaveFocus();
    });

    it('carries its own disabled visuals, since the Button base only styles native disabled', () => {
      renderManual();
      const createButton = screen.getByRole('button', { name: 'Create Task' });

      expect(createButton.className).toContain('aria-disabled:opacity-50');
      expect(createButton.className).toContain('aria-disabled:cursor-not-allowed');
      expect(createButton.className).toContain('aria-disabled:active:translate-y-0');
      expect(createButton.className).not.toContain('aria-disabled:pointer-events-none');
    });
  });
});
