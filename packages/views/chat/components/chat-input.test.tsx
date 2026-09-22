import { cloneElement, forwardRef, useEffect, useRef, useImperativeHandle } from 'react';
import { beforeEach, describe, it, expect, vi } from 'vitest';
import { act, render, screen, fireEvent, waitFor } from '@testing-library/react';
import { I18nProvider } from '@goosar/core/i18n/react';
import type { UploadResult } from '@goosar/core/hooks/use-file-upload';
import type { DraftUpload } from '@goosar/core/drafts';
import enCommon from '../../locales/en/common.json';
import enChat from '../../locales/en/chat.json';
import enEditor from '../../locales/en/editor.json';

const mockApiUploadFile = vi.hoisted(() => vi.fn());
const insertMarkdownSpy = vi.hoisted(() => vi.fn());

let mockUploadIdSeq = 0;

vi.mock('@goosar/core/api', () => ({
  api: { uploadFile: mockApiUploadFile },
}));

function makeUpload(
  overrides: Partial<UploadResult> & { id: string; link: string; filename: string },
): UploadResult {
  return {
    workspace_id: 'ws-1',
    issue_id: null,
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: 'member',
    uploader_id: 'user-1',
    url: overrides.link,
    download_url: overrides.link,
    markdown_url: overrides.link,
    content_type: 'image/png',
    size_bytes: 1,
    created_at: new Date(0).toISOString(),
    markdownLink: overrides.link,
    ...overrides,
  };
}

const TEST_RESOURCES = { en: { common: enCommon, chat: enChat, editor: enEditor } };

const dropHandlers = vi.hoisted(() => ({
  onDrop: null as null | ((files: File[]) => void),
}));
const editorProps = vi.hoisted(() => ({
  last: null as null | Record<string, unknown>,
}));
const editorState = vi.hoisted(() => ({ cleared: 0, blurred: 0, focused: 0 }));

vi.mock('../../editor', async () => ({
  ...(await vi.importActual<typeof import('../../editor/use-upload-gate')>(
    '../../editor/use-upload-gate',
  )),
  ...(await vi.importActual<typeof import('../../editor/use-composer-submit')>(
    '../../editor/use-composer-submit',
  )),
  useFileDropZone: ({ onDrop }: { onDrop: (files: File[]) => void }) => {
    dropHandlers.onDrop = onDrop;
    return { isDragOver: false, dropZoneProps: { 'data-testid': 'drop-zone' } };
  },
  FileDropOverlay: () => null,
  ContentEditor: forwardRef(function MockContentEditor(
    props: {
      defaultValue?: string;
      value?: string;
      onUpdate?: (md: string) => void;
      placeholder?: string;
      onUploadFile?: (file: File, uploadId: string) => Promise<UploadResult | null>;
      onUploadingChange?: (uploading: boolean) => void;
      mentionMode?: string;
      mentionContextItems?: unknown[];
    },
    ref: React.Ref<unknown>,
  ) {
    const { defaultValue, value, onUpdate, placeholder, onUploadFile, onUploadingChange } = props;
    editorProps.last = props as unknown as Record<string, unknown>;
    const valueRef = useRef<string>(value ?? defaultValue ?? '');
    const uploadingRef = useRef(0);
    useEffect(() => {
      if (value !== undefined) valueRef.current = value;
    }, [value]);
    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      clearContent: () => {
        editorState.cleared += 1;
        valueRef.current = '';
      },
      blur: () => {
        editorState.blurred += 1;
      },
      focus: () => {
        editorState.focused += 1;
      },
      uploadFile: async (file: File) => {
        uploadingRef.current += 1;
        if (uploadingRef.current === 1) onUploadingChange?.(true);
        try {
          const result = await onUploadFile?.(file, `mock-upload-${++mockUploadIdSeq}`);
          if (result) {
            const persistedURL = result.markdownLink || result.link;
            valueRef.current = `${valueRef.current}![](${persistedURL})`.trim();
            onUpdate?.(valueRef.current);
          }
        } finally {
          uploadingRef.current = Math.max(0, uploadingRef.current - 1);
          if (uploadingRef.current === 0) onUploadingChange?.(false);
        }
      },
      hasActiveUploads: () => uploadingRef.current > 0,
      insertUploadPlaceholder: () => true,
      settleUploadPlaceholder: () => false,
      insertMarkdownAtEnd: (md: string) => {
        insertMarkdownSpy(md);
        valueRef.current = `${valueRef.current}\n\n${md}`.trim();
        onUpdate?.(valueRef.current);
        return true;
      },
      flushPendingUpdate: () => null,
      adoptContent: (markdown: string) => {
        valueRef.current = markdown;
      },
    }));
    return (
      <textarea
        data-testid="editor"
        defaultValue={value ?? defaultValue}
        placeholder={placeholder}
        onChange={(e) => {
          valueRef.current = e.target.value;
          onUpdate?.(e.target.value);
        }}
      />
    );
  }),
}));

vi.mock('../../projects/components/project-picker', () => ({
  ProjectPicker: ({
    projectId,
    onUpdate,
    triggerRender,
    disabled,
  }: {
    projectId: string;
    onUpdate: (updates: { project_id: string | null }) => void;
    triggerRender: React.ReactElement<{
      onClick?: () => void;
      children?: React.ReactNode;
      'data-project-picker-disabled'?: string;
    }>;
    disabled?: boolean;
  }) =>
    cloneElement(triggerRender, {
      onClick: () => onUpdate({ project_id: null }),
      children: projectId,
      'data-project-picker-disabled': disabled ? 'true' : 'false',
    }),
}));

vi.mock('@goosar/core/chat', () => {
  const state = {
    activeSessionId: null as string | null,
    selectedAgentId: 'agent-1',
    inputDrafts: {} as Record<string, string>,
    inputDraftAttachments: {} as Record<string, unknown[]>,
    setInputDraft: vi.fn(),
    appendToInputDraft: vi.fn(),
    setInputDraftAttachments: vi.fn(),
    addInputDraftAttachment: vi.fn(),
    addInputDraftUpload: vi.fn(),
    settleInputDraftUpload: vi.fn(),
    failInputDraftUpload: vi.fn(),
    removeInputDraftUpload: vi.fn(),
    clearInputDraft: vi.fn(),
  };
  return {
    DRAFT_NEW_SESSION: '__draft_new__',
    useChatStore: Object.assign(
      (selector?: (s: typeof state) => unknown) => (selector ? selector(state) : state),
      { getState: () => state },
    ),
  };
});

import { ChatInput } from './chat-input';
import { useChatStore } from '@goosar/core/chat';

type ChatInputOnSend = React.ComponentProps<typeof ChatInput>['onSend'];
type ChatInputCommit = Parameters<ChatInputOnSend>[2];

beforeEach(() => {
  dropHandlers.onDrop = null;
  editorProps.last = null;
  editorState.cleared = 0;
  editorState.blurred = 0;
  editorState.focused = 0;
  const state = useChatStore.getState() as unknown as {
    activeSessionId: string | null;
    selectedAgentId: string;
    inputDrafts: Record<string, string>;
    setInputDraft: ReturnType<typeof vi.fn>;
    appendToInputDraft: ReturnType<typeof vi.fn>;
    clearInputDraft: ReturnType<typeof vi.fn>;
    inputDraftAttachments: Record<string, DraftUpload[]>;
    setInputDraftAttachments: ReturnType<typeof vi.fn>;
    addInputDraftAttachment: ReturnType<typeof vi.fn>;
    addInputDraftUpload: ReturnType<typeof vi.fn>;
    settleInputDraftUpload: ReturnType<typeof vi.fn>;
    failInputDraftUpload: ReturnType<typeof vi.fn>;
    removeInputDraftUpload: ReturnType<typeof vi.fn>;
  };
  state.activeSessionId = null;
  state.selectedAgentId = 'agent-1';
  state.inputDrafts = {};
  state.inputDraftAttachments = {};
  state.setInputDraft.mockClear();
  state.setInputDraft.mockImplementation((key: string, value: string) => {
    state.inputDrafts[key] = value;
  });
  state.appendToInputDraft.mockClear();
  state.appendToInputDraft.mockImplementation((key: string, markdown: string) => {
    const existing = state.inputDrafts[key] ?? '';
    state.inputDrafts[key] = existing.trim()
      ? `${existing.replace(/\s+$/, '')}\n\n${markdown}`
      : markdown;
  });
  state.setInputDraftAttachments.mockClear();
  state.setInputDraftAttachments.mockImplementation((key: string, uploads: DraftUpload[]) => {
    if (uploads.length > 0) state.inputDraftAttachments[key] = uploads;
    else delete state.inputDraftAttachments[key];
  });
  state.addInputDraftAttachment.mockClear();
  state.addInputDraftAttachment.mockImplementation((key: string, attachment: UploadResult) => {
    const existing = state.inputDraftAttachments[key] ?? [];
    state.inputDraftAttachments[key] = [
      ...existing,
      {
        clientUploadId: attachment.id,
        status: 'uploaded',
        filename: attachment.filename,
        size: attachment.size_bytes,
        attachment,
      } as DraftUpload,
    ];
  });
  state.addInputDraftUpload.mockClear();
  state.addInputDraftUpload.mockImplementation((key: string, upload: DraftUpload) => {
    const existing = state.inputDraftAttachments[key] ?? [];
    if (existing.some((u) => u.clientUploadId === upload.clientUploadId)) return;
    state.inputDraftAttachments[key] = [...existing, upload];
  });
  state.settleInputDraftUpload.mockClear();
  state.settleInputDraftUpload.mockImplementation(
    (key: string, clientUploadId: string, attachment: UploadResult) => {
      const existing = state.inputDraftAttachments[key] ?? [];
      state.inputDraftAttachments[key] = existing.map((u) =>
        u.clientUploadId === clientUploadId
          ? ({
              clientUploadId,
              status: 'uploaded',
              filename: attachment.filename,
              size: attachment.size_bytes,
              attachment,
            } as DraftUpload)
          : u,
      );
    },
  );
  state.failInputDraftUpload.mockClear();
  state.failInputDraftUpload.mockImplementation(
    (key: string, clientUploadId: string, error?: string) => {
      const existing = state.inputDraftAttachments[key] ?? [];
      state.inputDraftAttachments[key] = existing.map((u) =>
        u.clientUploadId === clientUploadId
          ? ({ ...u, status: 'failed', error } as DraftUpload)
          : u,
      );
    },
  );
  state.removeInputDraftUpload.mockClear();
  state.removeInputDraftUpload.mockImplementation((key: string, clientUploadId: string) => {
    const remaining = (state.inputDraftAttachments[key] ?? []).filter(
      (u) => u.clientUploadId !== clientUploadId,
    );
    if (remaining.length > 0) state.inputDraftAttachments[key] = remaining;
    else delete state.inputDraftAttachments[key];
  });
  state.clearInputDraft.mockClear();
  state.clearInputDraft.mockImplementation((key: string) => {
    delete state.inputDrafts[key];
    delete state.inputDraftAttachments[key];
  });
  mockApiUploadFile.mockReset();
  mockApiUploadFile.mockImplementation(async () =>
    makeUpload({ id: 'att-1', link: 'https://cdn.example/att-1.png', filename: 'img.png' }),
  );
  insertMarkdownSpy.mockReset();
});

function renderInput(props: Partial<React.ComponentProps<typeof ChatInput>> = {}) {
  const onSend = props.onSend ?? vi.fn();
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ChatInput onSend={onSend} uploadEnabled agentName="Goosar" {...props} />
    </I18nProvider>,
  );
  return { onSend };
}

function element(props: Partial<React.ComponentProps<typeof ChatInput>>) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ChatInput onSend={vi.fn()} uploadEnabled agentName="Goosar" {...props} />
    </I18nProvider>
  );
}

describe('ChatInput new-chat draft identity', () => {
  function switchAgentTo(agentId: string, rerender: (ui: React.ReactElement) => void) {
    const state = useChatStore.getState() as unknown as { selectedAgentId: string };
    state.selectedAgentId = agentId;
    rerender(element({ agentName: agentId }));
  }

  it('writes to the single new-chat slot regardless of the selected agent', () => {
    const { rerender } = render(element({ agentName: 'agent-1' }));

    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'half a thought' } });
    switchAgentTo('agent-2', rerender);
    fireEvent.change(screen.getByTestId('editor'), {
      target: { value: 'half a thought, finished' },
    });

    const state = useChatStore.getState() as unknown as { inputDrafts: Record<string, string> };
    expect(Object.keys(state.inputDrafts)).toEqual(['__draft_new__']);
    expect(state.inputDrafts['__draft_new__']).toBe('half a thought, finished');
  });

  it('keeps the live editor instance across an agent switch', () => {
    const { rerender } = render(element({ agentName: 'agent-1' }));
    const before = screen.getByTestId('editor');

    switchAgentTo('agent-2', rerender);

    expect(screen.getByTestId('editor')).toBe(before);
  });

  it('keeps text the draft debounce has not persisted yet across an agent switch', () => {
    const { rerender } = render(element({ agentName: 'agent-1' }));
    const editor = screen.getByTestId('editor') as HTMLTextAreaElement;
    fireEvent.change(editor, { target: { value: 'unsaved words' } });

    switchAgentTo('agent-2', rerender);

    expect((screen.getByTestId('editor') as HTMLTextAreaElement).value).toBe('unsaved words');
  });

  it('keeps staged attachments across an agent switch', async () => {
    mockApiUploadFile.mockImplementation(async () =>
      makeUpload({ id: 'att-kept', link: '/api/attachments/att-kept/download', filename: 'a.png' }),
    );
    const { rerender } = render(element({ agentName: 'agent-1' }));

    await act(async () => {
      dropHandlers.onDrop?.([new File(['x'], 'a.png', { type: 'image/png' })]);
      await Promise.resolve();
    });
    switchAgentTo('agent-2', rerender);

    const state = useChatStore.getState() as unknown as {
      inputDraftAttachments: Record<string, DraftUpload[]>;
    };
    expect(
      state.inputDraftAttachments['__draft_new__']?.map((u) =>
        u.status === 'uploaded' ? u.attachment.id : u.status,
      ),
    ).toEqual(['att-kept']);
    expect(Object.keys(state.inputDraftAttachments)).toEqual(['__draft_new__']);
  });

  it('still gives each created session its own draft slot', () => {
    const state = useChatStore.getState() as unknown as {
      activeSessionId: string | null;
      inputDrafts: Record<string, string>;
    };
    state.activeSessionId = 'session-a';
    const { rerender } = render(element({ agentName: 'agent-1' }));
    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'for A' } });

    state.activeSessionId = 'session-b';
    rerender(element({ agentName: 'agent-1' }));
    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'for B' } });

    expect(state.inputDrafts).toEqual({ 'session-a': 'for A', 'session-b': 'for B' });
  });
});

describe('ChatInput focusRequest', () => {
  it('focuses the editor when focusRequest becomes a non-zero value (new chat)', () => {
    const { rerender } = render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ChatInput onSend={vi.fn()} agentName="Goosar" focusRequest={0} />
      </I18nProvider>,
    );
    expect(editorState.focused).toBe(0);

    rerender(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ChatInput onSend={vi.fn()} agentName="Goosar" focusRequest={1} />
      </I18nProvider>,
    );
    expect(editorState.focused).toBe(1);

    rerender(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ChatInput onSend={vi.fn()} agentName="Goosar" focusRequest={2} />
      </I18nProvider>,
    );
    expect(editorState.focused).toBe(2);
  });

  it('does not focus on mount when focusRequest is undefined or 0', () => {
    renderInput();
    expect(editorState.focused).toBe(0);
  });
});

describe('ChatInput @ context wiring', () => {
  it('configures chat @ with current/recent issue/project context', () => {
    const contextItems = [
      { id: 'issue-1', label: 'MUL-1', type: 'issue' as const, group: 'current' as const },
    ];

    renderInput({ contextItems });

    expect(editorProps.last?.mentionMode).toBe('context');
    expect(editorProps.last?.mentionContextItems).toBe(contextItems);
  });
});

describe('ChatInput project context', () => {
  type ChatProject = NonNullable<React.ComponentProps<typeof ChatInput>['projects']>[number];
  const sampleProject: ChatProject = {
    id: 'project-alpha',
    workspace_id: 'ws-1',
    title: 'Project Alpha',
    description: null,
    icon: '📘',
    status: 'planned',
    priority: 'none',
    lead_type: null,
    lead_id: null,
    start_date: null,
    due_date: null,
    created_at: new Date(0).toISOString(),
    updated_at: new Date(0).toISOString(),
    issue_count: 0,
    done_count: 0,
    resource_count: 0,
  };

  it("warns next to the chip when the agent's daemon cannot apply the project description", () => {
    renderInput({
      projects: [sampleProject],
      projectId: 'project-alpha',
      onProjectChange: vi.fn(),
      projectContextUnsupported: true,
    });

    expect(
      screen.getByText("Project description won't apply — this agent's daemon needs an upgrade"),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Change project context' })).not.toBeDisabled();
  });

  it('shows no daemon warning when support is current or unknown', () => {
    renderInput({
      projects: [sampleProject],
      projectId: 'project-alpha',
      onProjectChange: vi.fn(),
    });

    expect(
      screen.queryByText("Project description won't apply — this agent's daemon needs an upgrade"),
    ).not.toBeInTheDocument();
  });

  it('renders the selected project chip and forwards context changes', () => {
    const onProjectChange = vi.fn();
    renderInput({
      projects: [sampleProject],
      projectId: 'project-alpha',
      onProjectChange,
    });

    fireEvent.click(screen.getByRole('button', { name: 'Change project context' }));

    expect(onProjectChange).toHaveBeenCalledWith(null);
  });

  it('allows removing project context while the agent is running', () => {
    const onProjectChange = vi.fn();
    renderInput({
      projects: [sampleProject],
      projectId: 'project-alpha',
      onProjectChange,
      isRunning: true,
    });

    const projectControl = screen.getByRole('button', {
      name: 'Change project context',
    });
    expect(projectControl).not.toBeDisabled();
    fireEvent.click(projectControl);
    expect(onProjectChange).toHaveBeenCalledWith(null);
  });

  it('locks the project control while a send is in flight so a mid-send switch cannot retarget the session', async () => {
    let resolveSend: (accepted: boolean) => void;
    const sendPromise = new Promise<boolean>((res) => {
      resolveSend = res;
    });
    const onSend = vi.fn<ChatInputOnSend>(() => sendPromise);
    const onProjectChange = vi.fn();
    renderInput({
      projects: [sampleProject],
      projectId: 'project-alpha',
      onProjectChange,
      onSend,
    });

    expect(screen.getByRole('button', { name: 'Change project context' })).not.toBeDisabled();

    fireEvent.change(screen.getByTestId('editor'), {
      target: { value: 'slow network' },
    });
    let sendBtn: HTMLElement;
    await waitFor(() => {
      const buttons = screen.getAllByRole('button');
      sendBtn = buttons[buttons.length - 1]!;
      expect(sendBtn).not.toBeDisabled();
    });
    fireEvent.click(sendBtn!);

    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Change project context' })).toBeDisabled(),
    );
    expect(screen.getByRole('button', { name: 'Change project context' })).toHaveAttribute(
      'data-project-picker-disabled',
      'true',
    );
    fireEvent.click(screen.getByRole('button', { name: 'Change project context' }));
    expect(onProjectChange).not.toHaveBeenCalled();

    await act(async () => {
      resolveSend!(true);
      await sendPromise;
    });
  });
});

describe('ChatInput attachment wiring', () => {
  it('routes dropped files through the coordinator upload', async () => {
    renderInput();
    expect(dropHandlers.onDrop).not.toBeNull();
    const file = new File(['x'], 'drop.png', { type: 'image/png' });
    await act(async () => {
      dropHandlers.onDrop?.([file]);
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(mockApiUploadFile).toHaveBeenCalledTimes(1);
    expect(mockApiUploadFile.mock.calls[0]?.[0]).toBe(file);
  });

  it('passes attachment_ids to onSend for uploads still referenced in the content', async () => {
    const onSend = vi.fn();
    mockApiUploadFile.mockImplementation(async () =>
      makeUpload({ id: 'att-42', link: 'https://cdn.example/att-42.png', filename: 'x.png' }),
    );
    renderInput({ onSend });

    const file = new File(['x'], 'drop.png', { type: 'image/png' });
    await act(async () => {
      dropHandlers.onDrop?.([file]);
      await Promise.resolve();
      await Promise.resolve();
    });

    let sendButton: HTMLElement;
    await waitFor(() => {
      const buttons = screen.getAllByRole('button');
      sendButton = buttons[buttons.length - 1]!;
      expect(sendButton).not.toBeDisabled();
    });
    fireEvent.click(sendButton!);

    expect(onSend).toHaveBeenCalledTimes(1);
    const [, ids] = onSend.mock.calls[0]!;
    expect(ids).toEqual(['att-42']);
    expect(useChatStore.getState().addInputDraftUpload).toHaveBeenCalledWith(
      '__draft_new__',
      expect.objectContaining({ status: 'uploading', filename: 'drop.png' }),
    );
  });

  it("binds attachment_ids when the upload's markdownLink differs from its link (MUL-3130 regression)", async () => {
    const onSend = vi.fn();
    const SHORT_LIVED_LINK = '/uploads/workspaces/ws-1/foo.png?exp=42&sig=stale';
    const STABLE_MARKDOWN_LINK = '/api/attachments/att-99/download';
    mockApiUploadFile.mockImplementation(async () =>
      makeUpload({
        id: 'att-99',
        link: SHORT_LIVED_LINK,
        markdown_url: STABLE_MARKDOWN_LINK,
        markdownLink: STABLE_MARKDOWN_LINK,
        filename: 'foo.png',
      }),
    );
    renderInput({ onSend });

    const file = new File(['x'], 'foo.png', { type: 'image/png' });
    await act(async () => {
      dropHandlers.onDrop?.([file]);
      await Promise.resolve();
      await Promise.resolve();
    });

    let sendButton: HTMLElement;
    await waitFor(() => {
      const buttons = screen.getAllByRole('button');
      sendButton = buttons[buttons.length - 1]!;
      expect(sendButton).not.toBeDisabled();
    });
    fireEvent.click(sendButton!);

    expect(onSend).toHaveBeenCalledTimes(1);
    const [content, ids] = onSend.mock.calls[0]!;
    expect(content).toContain(STABLE_MARKDOWN_LINK);
    expect(content).not.toContain('?exp=');
    expect(content).not.toContain('?sig=');
    expect(ids).toEqual(['att-99']);
  });

  it('disables send while an upload is in flight, re-enables after it resolves', async () => {
    let resolveUpload: (v: UploadResult) => void;
    const uploadPromise = new Promise<UploadResult>((res) => {
      resolveUpload = res;
    });
    const onSend = vi.fn();
    mockApiUploadFile.mockImplementation(() => uploadPromise);
    renderInput({ onSend });

    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'preview text' } });

    const file = new File(['x'], 'slow.png', { type: 'image/png' });
    await act(async () => {
      dropHandlers.onDrop?.([file]);
      await Promise.resolve();
    });

    await waitFor(() => {
      const buttons = screen.getAllByRole('button');
      const sendButton = buttons[buttons.length - 1]!;
      expect(sendButton).toBeDisabled();
    });

    await act(async () => {
      resolveUpload!(
        makeUpload({
          id: 'att-slow',
          link: 'https://cdn.example/att-slow.png',
          filename: 'slow.png',
        }),
      );
      await Promise.resolve();
    });

    let sendButton: HTMLElement;
    await waitFor(() => {
      const buttons = screen.getAllByRole('button');
      sendButton = buttons[buttons.length - 1]!;
      expect(sendButton).not.toBeDisabled();
    });
    fireEvent.click(sendButton!);
    expect(onSend).toHaveBeenCalledTimes(1);
    const [, ids] = onSend.mock.calls[0]!;
    expect(ids).toEqual(['att-slow']);
  });

  it("delivers a dead mount's settle into the editor still HOLDING that draft, not the selected one", async () => {
    const state = useChatStore.getState() as unknown as {
      activeSessionId: string | null;
      inputDrafts: Record<string, string>;
      inputDraftAttachments: Record<string, DraftUpload[]>;
    };

    state.activeSessionId = 'session-a';
    let resolveA!: (v: UploadResult) => void;
    mockApiUploadFile.mockImplementationOnce(
      () => new Promise<UploadResult>((r) => (resolveA = r)),
    );
    const first = render(element({}));
    await act(async () => {
      dropHandlers.onDrop?.([new File(['x'], 'a.png', { type: 'image/png' })]);
      await Promise.resolve();
    });
    first.unmount();

    let resolveB!: (v: UploadResult) => void;
    mockApiUploadFile.mockImplementationOnce(
      () => new Promise<UploadResult>((r) => (resolveB = r)),
    );
    const second = render(element({}));
    await act(async () => {
      dropHandlers.onDrop?.([new File(['y'], 'b.png', { type: 'image/png' })]);
      await Promise.resolve();
    });
    state.activeSessionId = 'session-b';
    second.rerender(element({}));

    await act(async () => {
      resolveA(
        makeUpload({
          id: 'att-pinned',
          link: '/api/attachments/att-pinned/download',
          filename: 'a.png',
        }),
      );
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(insertMarkdownSpy).toHaveBeenCalledWith(
      expect.stringContaining('/api/attachments/att-pinned/download'),
    );
    expect(state.inputDrafts['session-a'] ?? '').toContain('/api/attachments/att-pinned/download');
    expect(state.inputDrafts['session-b'] ?? '').not.toContain('att-pinned');

    await act(async () => {
      resolveB(
        makeUpload({ id: 'att-b', link: '/api/attachments/att-b/download', filename: 'b.png' }),
      );
      await Promise.resolve();
    });
  });

  it('text typed while a chat send is in flight survives the success', async () => {
    const state = useChatStore.getState() as unknown as {
      inputDrafts: Record<string, string>;
    };
    let resolveSend!: (v: boolean) => void;
    const onSend = vi.fn(
      (
        _content: string,
        _ids: string[] | undefined,
        _commit: (o?: { extraDraftKeys?: string[]; clearEditor?: boolean }) => void,
      ) =>
        new Promise<boolean>((r) => {
          resolveSend = r;
        }),
    );
    renderInput({ onSend: onSend as never });

    const editor = screen.getByTestId('editor');
    fireEvent.change(editor, { target: { value: 'draft A' } });
    const buttons = screen.getAllByRole('button');
    fireEvent.click(buttons[buttons.length - 1]!);
    await waitFor(() => expect(onSend).toHaveBeenCalled());

    fireEvent.change(editor, { target: { value: 'draft B typed during send' } });

    await act(async () => {
      resolveSend(true);
      await Promise.resolve();
    });

    expect(state.inputDrafts['__draft_new__']).toBe('draft B typed during send');
    expect(editorState.cleared).toBe(0);
  });

  it('keeps an in-flight placeholder while the user keeps typing', () => {
    const state = useChatStore.getState() as unknown as {
      inputDraftAttachments: Record<string, DraftUpload[]>;
    };
    state.inputDraftAttachments['__draft_new__'] = [
      { clientUploadId: 'c-flight', status: 'uploading', filename: 'up.png', size: 1 },
    ];
    renderInput();

    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'still typing' } });

    expect(state.inputDraftAttachments['__draft_new__']).toHaveLength(1);
    expect(state.inputDraftAttachments['__draft_new__']?.[0]).toMatchObject({
      status: 'uploading',
      clientUploadId: 'c-flight',
    });
  });

  it('does not render the file upload button when uploads are disabled', () => {
    renderInput({ uploadEnabled: false });
    const buttons = screen.getAllByRole('button');
    expect(buttons.length).toBe(1);
  });
});

describe('ChatInput async send', () => {
  it('restores a cancelled empty run draft into the editor', async () => {
    const onRestoreDraftApplied = vi.fn();
    renderInput({
      restoreDraftRequest: {
        id: 'msg-restored',
        content: 'bring this back',
      },
      onRestoreDraftApplied,
    });

    await waitFor(() => {
      expect(useChatStore.getState().setInputDraft).toHaveBeenCalledWith(
        '__draft_new__',
        'bring this back',
      );
      expect(editorProps.last?.value).toBe('bring this back');
      expect(onRestoreDraftApplied).toHaveBeenCalledTimes(1);
    });
  });

  it('waits — does not report — while an existing draft blocks the restore', async () => {
    const state = useChatStore.getState() as unknown as {
      inputDrafts: Record<string, string>;
      setInputDraft: ReturnType<typeof vi.fn>;
    };
    state.inputDrafts['__draft_new__'] = 'already typing';
    const onRestoreDraftApplied = vi.fn();

    const { rerender } = render(
      element({
        restoreDraftRequest: { id: 'msg-restored', content: 'bring this back' },
        onRestoreDraftApplied,
      }),
    );

    await waitFor(() => {
      expect(editorProps.last?.value).toBe('already typing');
    });
    expect(onRestoreDraftApplied).not.toHaveBeenCalled();
    expect(state.setInputDraft).not.toHaveBeenCalledWith('__draft_new__', 'bring this back');

    state.inputDrafts['__draft_new__'] = '';
    rerender(
      element({
        restoreDraftRequest: { id: 'msg-restored', content: 'bring this back' },
        onRestoreDraftApplied,
      }),
    );

    await waitFor(() => {
      expect(state.setInputDraft).toHaveBeenCalledWith('__draft_new__', 'bring this back');
      expect(onRestoreDraftApplied).toHaveBeenCalledTimes(1);
    });
  });

  it('holds the restore when the draft has staged attachments but no text', async () => {
    const state = useChatStore.getState() as unknown as {
      inputDraftAttachments: Record<string, { id: string }[]>;
      setInputDraft: ReturnType<typeof vi.fn>;
      setInputDraftAttachments: ReturnType<typeof vi.fn>;
    };
    state.inputDraftAttachments['__draft_new__'] = [{ id: 'att-staged' }];
    const onRestoreDraftApplied = vi.fn();

    renderInput({
      restoreDraftRequest: {
        id: 'msg-restored',
        content: 'bring this back',
      },
      onRestoreDraftApplied,
    });

    await waitFor(() => {
      expect(editorProps.last).toBeTruthy();
    });
    expect(onRestoreDraftApplied).not.toHaveBeenCalled();
    expect(state.setInputDraftAttachments).not.toHaveBeenCalled();
    expect(state.setInputDraft).not.toHaveBeenCalledWith('__draft_new__', 'bring this back');
  });

  it('keeps the draft while send is pending until the owner commits the handoff', async () => {
    let resolveSend: (accepted: boolean) => void;
    const sendPromise = new Promise<boolean>((res) => {
      resolveSend = res;
    });
    const onSend = vi.fn<ChatInputOnSend>(() => sendPromise);
    renderInput({ onSend });

    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'slow network' } });

    let sendButton: HTMLElement;
    await waitFor(() => {
      const buttons = screen.getAllByRole('button');
      sendButton = buttons[buttons.length - 1]!;
      expect(sendButton).not.toBeDisabled();
    });

    fireEvent.click(sendButton!);

    expect(onSend).toHaveBeenCalledWith('slow network', undefined, expect.any(Function), []);
    expect(useChatStore.getState().clearInputDraft).not.toHaveBeenCalled();
    await waitFor(() => expect(sendButton!).toBeDisabled());

    const commitInput = onSend.mock.calls[0]![2] as ChatInputCommit;
    act(() => {
      commitInput({ extraDraftKeys: ['session-1'] });
    });

    expect(useChatStore.getState().clearInputDraft).toHaveBeenCalledWith('__draft_new__');
    expect(useChatStore.getState().clearInputDraft).toHaveBeenCalledWith('session-1');

    await act(async () => {
      resolveSend!(true);
      await sendPromise;
    });

    expect(useChatStore.getState().clearInputDraft).toHaveBeenCalledTimes(2);
  });

  it('keeps the draft when send is rejected by the owner', async () => {
    const onSend = vi.fn(async () => false);
    renderInput({ onSend });

    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'retry me' } });

    let sendButton: HTMLElement;
    await waitFor(() => {
      const buttons = screen.getAllByRole('button');
      sendButton = buttons[buttons.length - 1]!;
      expect(sendButton).not.toBeDisabled();
    });

    await act(async () => {
      fireEvent.click(sendButton!);
      await Promise.resolve();
    });

    expect(onSend).toHaveBeenCalledWith('retry me', undefined, expect.any(Function), []);
    expect(useChatStore.getState().clearInputDraft).not.toHaveBeenCalled();
  });

  it('sends attachment ids restored from persisted draft attachments', async () => {
    const state = useChatStore.getState() as unknown as {
      inputDrafts: Record<string, string>;
      inputDraftAttachments: Record<string, DraftUpload[]>;
    };
    const attachment = makeUpload({
      id: 'att-persisted',
      link: '/api/attachments/att-persisted/download',
      filename: 'persisted.png',
    });
    state.inputDrafts['__draft_new__'] = 'see ![](/api/attachments/att-persisted/download)';
    state.inputDraftAttachments['__draft_new__'] = [
      {
        clientUploadId: 'att-persisted',
        status: 'uploaded',
        filename: attachment.filename,
        size: attachment.size_bytes,
        attachment,
      },
    ];

    const onSend = vi.fn<ChatInputOnSend>((_content, _ids, commitInput) => {
      commitInput();
      return true;
    });
    renderInput({ onSend });

    let sendButton: HTMLElement;
    await waitFor(() => {
      const buttons = screen.getAllByRole('button');
      sendButton = buttons[buttons.length - 1]!;
      expect(sendButton).not.toBeDisabled();
    });

    fireEvent.click(sendButton!);

    expect(onSend).toHaveBeenCalledWith(
      'see ![](/api/attachments/att-persisted/download)',
      ['att-persisted'],
      expect.any(Function),
      [attachment],
    );
  });
});

describe('ChatInput send affordance', () => {
  function sendButton() {
    const buttons = screen.getAllByRole('button');
    return buttons[buttons.length - 1]!;
  }

  it('enables Send for a draft that arrived from the store, not from typing', async () => {
    const state = useChatStore.getState() as unknown as {
      activeSessionId: string | null;
      inputDrafts: Record<string, string>;
    };
    state.activeSessionId = 'session-a';
    state.inputDrafts = { 'session-b': 'the text that failed to send' };

    const { rerender } = render(element({}));
    expect(sendButton()).toBeDisabled();

    state.activeSessionId = 'session-b';
    rerender(element({}));

    await waitFor(() => expect(sendButton()).not.toBeDisabled());
  });

  it('stays disabled when neither the editor nor the draft slot has content', async () => {
    const state = useChatStore.getState() as unknown as {
      activeSessionId: string | null;
      inputDrafts: Record<string, string>;
    };
    state.activeSessionId = 'session-b';
    state.inputDrafts = {};

    renderInput();

    await waitFor(() => expect(sendButton()).toBeDisabled());
  });

  it('keeps Send enabled when a lazy session create flips the draft key under a typed editor', async () => {
    const state = useChatStore.getState() as unknown as {
      activeSessionId: string | null;
      inputDrafts: Record<string, string>;
    };
    state.activeSessionId = null;
    state.inputDrafts = {};

    const { rerender } = render(element({}));
    fireEvent.change(screen.getByTestId('editor'), {
      target: { value: 'typed before the session existed' },
    });
    await waitFor(() => expect(sendButton()).not.toBeDisabled());

    state.activeSessionId = 'session-new';
    rerender(element({}));

    expect(sendButton()).not.toBeDisabled();
  });
});

describe('ChatInput session-aware restore', () => {
  it('holds a session-scoped restore until the user returns to the source session', async () => {
    const state = useChatStore.getState() as unknown as {
      activeSessionId: string | null;
      setInputDraft: ReturnType<typeof vi.fn>;
    };
    state.activeSessionId = 'session-b';
    const onRestoreDraftApplied = vi.fn();
    const props = {
      restoreDraftRequest: { id: 'r1', content: 'from A', sessionId: 'session-a' },
      onRestoreDraftApplied,
    };
    const { rerender } = render(element(props));

    expect(onRestoreDraftApplied).not.toHaveBeenCalled();
    expect(state.setInputDraft).not.toHaveBeenCalledWith('session-b', 'from A');

    state.activeSessionId = 'session-a';
    rerender(element(props));

    await waitFor(() => {
      expect(state.setInputDraft).toHaveBeenCalledWith('session-a', 'from A');
      expect(onRestoreDraftApplied).toHaveBeenCalledTimes(1);
    });
  });

  it('consumes a session-scoped restore when already on that session', async () => {
    const state = useChatStore.getState() as unknown as {
      activeSessionId: string | null;
      setInputDraft: ReturnType<typeof vi.fn>;
    };
    state.activeSessionId = 'session-a';
    const onRestoreDraftApplied = vi.fn();
    render(
      element({
        restoreDraftRequest: { id: 'r2', content: 'hi A', sessionId: 'session-a' },
        onRestoreDraftApplied,
      }),
    );

    await waitFor(() => {
      expect(state.setInputDraft).toHaveBeenCalledWith('session-a', 'hi A');
      expect(onRestoreDraftApplied).toHaveBeenCalledTimes(1);
    });
  });
});

describe('ChatInput commit handoff', () => {
  async function typeAndSend(onSend: ChatInputOnSend) {
    renderInput({ onSend });
    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'msg' } });
    let sendButton: HTMLElement;
    await waitFor(() => {
      const buttons = screen.getAllByRole('button');
      sendButton = buttons[buttons.length - 1]!;
      expect(sendButton).not.toBeDisabled();
    });
    fireEvent.click(sendButton!);
    await waitFor(() => expect(onSend).toHaveBeenCalled());
  }

  it('scrubs the editor and clears the draft on a normal commit', async () => {
    const onSend = vi.fn<ChatInputOnSend>((_content, _ids, commitInput) => {
      commitInput();
      return true;
    });
    await typeAndSend(onSend);

    expect(editorState.cleared).toBeGreaterThan(0);
    expect(useChatStore.getState().clearInputDraft).toHaveBeenCalledWith('__draft_new__');
    await waitFor(() => expect(editorState.focused).toBeGreaterThan(0));
    expect(editorState.blurred).toBe(0);
  });

  it('leaves the editor intact on a fire-and-forget commit but still clears the sent draft', async () => {
    const onSend = vi.fn<ChatInputOnSend>((_content, _ids, commitInput) => {
      commitInput({ clearEditor: false });
      return true;
    });
    await typeAndSend(onSend);

    expect(editorState.cleared).toBe(0);
    expect(editorState.blurred).toBe(0);
    expect(useChatStore.getState().clearInputDraft).toHaveBeenCalledWith('__draft_new__');
    await new Promise((resolve) => requestAnimationFrame(() => resolve(null)));
    expect(editorState.focused).toBe(0);
  });
});
