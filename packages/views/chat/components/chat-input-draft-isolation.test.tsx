import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { I18nProvider } from '@goosar/core/i18n/react';
import type { UploadResult } from '@goosar/core/hooks/use-file-upload';
import enCommon from '../../locales/en/common.json';
import enChat from '../../locales/en/chat.json';
import enEditor from '../../locales/en/editor.json';

const mockApiUploadFile = vi.hoisted(() => vi.fn());

vi.mock('@goosar/core/api', () => ({
  api: { uploadFile: mockApiUploadFile },
}));

const editorState = vi.hoisted(() => ({
  isFocused: false,
  isDestroyed: false,
  markdown: '',
  uploadingNodes: [] as Array<{ attrs: { uploading?: boolean } }>,
}));
const editorInstance = vi.hoisted<{ current: unknown }>(() => ({ current: null }));
const onCreateFired = vi.hoisted(() => ({ value: false }));
const transactionListeners = vi.hoisted(() => ({ current: [] as Array<() => void> }));
const latestEditorOptions = vi.hoisted<{
  current?: { onUpdate?: (args: { editor: unknown }) => void };
}>(() => ({}));
const mockSetContent = vi.hoisted(() => vi.fn());

vi.mock('@tanstack/react-query', () => ({
  useQueryClient: () => ({}),
}));
const capturedExtOptions = vi.hoisted<{
  current?: { onSubmitRef?: { current?: () => void } };
}>(() => ({}));
vi.mock('../../editor/extensions', () => ({
  createEditorExtensions: (options: { onSubmitRef?: { current?: () => void } }) => {
    capturedExtOptions.current = options;
    return [];
  },
}));
vi.mock('../../editor/extensions/file-upload', async () => ({
  ...(await vi.importActual<typeof import('../../editor/extensions/file-upload')>(
    '../../editor/extensions/file-upload',
  )),
  uploadAndInsertFile: async (
    _editor: unknown,
    file: File,
    handler: (f: File) => Promise<unknown>,
  ) => {
    await handler(file);
  },
}));
vi.mock('../../editor/utils/preprocess', () => ({
  preprocessMarkdown: (value: string) => value,
}));
vi.mock('../../editor/utils/repair-list-items', () => ({
  repairEmptyListItems: vi.fn(() => false),
}));
vi.mock('../../editor/bubble-menu', () => ({
  EditorBubbleMenu: () => null,
}));
vi.mock('../../editor/attachment-download-context', () => ({
  AttachmentDownloadProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

vi.mock('@tiptap/react', () => ({
  useEditor: (options: {
    onCreate?: (args: { editor: unknown }) => void;
    onUpdate?: (args: { editor: unknown }) => void;
  }) => {
    latestEditorOptions.current = options;
    if (!editorInstance.current) {
      editorInstance.current = {
        get isFocused() {
          return editorState.isFocused;
        },
        get isDestroyed() {
          return editorState.isDestroyed;
        },
        commands: {
          focus: vi.fn(),
          blur: vi.fn(),
          clearContent: vi.fn(() => {
            editorState.markdown = '';
          }),
          setContent: mockSetContent,
          setTextSelection: vi.fn(),
        },
        getMarkdown: () => editorState.markdown,
        on: (event: string, cb: () => void) => {
          if (event === 'transaction') transactionListeners.current.push(cb);
        },
        off: (event: string, cb: () => void) => {
          if (event !== 'transaction') return;
          transactionListeners.current = transactionListeners.current.filter((l) => l !== cb);
        },
        view: { dispatch: vi.fn() },
        state: {
          get tr() {
            return { __emptyTransaction: true };
          },
          doc: {
            content: { size: 0 },
            descendants: (cb: (node: { attrs: { uploading?: boolean } }) => boolean | void) => {
              for (const node of editorState.uploadingNodes) {
                if (cb(node) === false) break;
              }
            },
          },
          selection: { empty: true, from: 0, to: 0 },
        },
      };
    }
    if (!onCreateFired.value) {
      onCreateFired.value = true;
      options?.onCreate?.({ editor: editorInstance.current });
    }
    return editorInstance.current;
  },
  EditorContent: ({ className }: { className?: string }) => (
    <div className={className} data-testid="editor-content" />
  ),
}));

vi.mock('@goosar/core/chat', () => {
  const state = {
    activeSessionId: null as string | null,
    selectedAgentId: 'agent-1',
    inputDrafts: {} as Record<string, string>,
    inputDraftAttachments: {} as Record<string, unknown[]>,
    setInputDraft: vi.fn((key: string, value: string) => {
      state.inputDrafts[key] = value;
    }),
    setInputDraftAttachments: vi.fn(),
    addInputDraftAttachment: vi.fn(),
    appendToInputDraft: vi.fn(),
    addInputDraftUpload: vi.fn((key: string, upload: { clientUploadId: string }) => {
      const existing = state.inputDraftAttachments[key] ?? [];
      state.inputDraftAttachments[key] = [...existing, upload];
    }),
    settleInputDraftUpload: vi.fn(),
    failInputDraftUpload: vi.fn(),
    removeInputDraftUpload: vi.fn(),
    clearInputDraft: vi.fn(),
  };
  return {
    DRAFT_NEW_SESSION: '__new__',
    useChatStore: Object.assign(
      (selector?: (s: typeof state) => unknown) => (selector ? selector(state) : state),
      { getState: () => state },
    ),
  };
});

import { ChatInput } from './chat-input';
import { useChatStore } from '@goosar/core/chat';

const TEST_RESOURCES = { en: { common: enCommon, chat: enChat, editor: enEditor } };

function makeUpload(id: string, filename: string): UploadResult {
  const link = `/api/attachments/${id}/download`;
  return {
    id,
    filename,
    workspace_id: 'ws-1',
    issue_id: null,
    comment_id: null,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: 'member',
    uploader_id: 'user-1',
    url: link,
    download_url: link,
    markdown_url: link,
    content_type: 'image/png',
    size_bytes: 1,
    created_at: new Date(0).toISOString(),
    markdownLink: link,
    link,
  };
}

function store() {
  return useChatStore.getState() as unknown as {
    activeSessionId: string | null;
    selectedAgentId: string;
    inputDrafts: Record<string, string>;
    inputDraftAttachments: Record<string, unknown[]>;
  };
}

function element(props: Partial<React.ComponentProps<typeof ChatInput>> = {}) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ChatInput onSend={vi.fn()} agentName="Goosar" {...props} />
    </I18nProvider>
  );
}

function emitTransaction() {
  act(() => {
    for (const listener of [...transactionListeners.current]) listener();
  });
}

function beginUpload(markdown: string) {
  editorState.uploadingNodes = [{ attrs: { uploading: true } }];
  type(markdown);
  emitTransaction();
}

function finishUpload(markdown: string) {
  editorState.uploadingNodes = [];
  type(markdown);
  emitTransaction();
}

function type(markdown: string) {
  editorState.markdown = markdown;
  act(() => {
    latestEditorOptions.current?.onUpdate?.({ editor: editorInstance.current });
  });
}

describe('ChatInput draft isolation across a composer switch (real debounce)', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    editorState.isFocused = true;
    editorState.isDestroyed = false;
    editorState.markdown = '';
    editorState.uploadingNodes = [];
    editorInstance.current = null;
    onCreateFired.value = false;
    latestEditorOptions.current = undefined;
    mockSetContent.mockClear();
    const s = store();
    s.activeSessionId = null;
    s.selectedAgentId = 'agent-1';
    s.inputDrafts = {};
    s.inputDraftAttachments = {};
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('files unflushed keystrokes under the session they were typed in, never the one switched to', () => {
    store().activeSessionId = 'session-a';
    const { rerender } = render(element());

    type('secret plan for agent A');
    expect(store().inputDrafts).toEqual({});

    store().activeSessionId = 'session-b';
    rerender(element());

    expect(store().inputDrafts['session-a']).toBe('secret plan for agent A');
    expect(store().inputDrafts['session-b']).toBeUndefined();

    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(store().inputDrafts['session-b']).toBeUndefined();
    expect(store().inputDrafts['session-a']).toBe('secret plan for agent A');
  });

  it("loads the incoming session's own draft instead of leaving the old document on screen", () => {
    store().activeSessionId = 'session-a';
    store().inputDrafts['session-b'] = "B's own words";
    const { rerender } = render(element());

    type("A's unflushed words");
    store().activeSessionId = 'session-b';
    rerender(element());

    expect(mockSetContent).toHaveBeenCalled();
  });

  it('keeps a New Chat draft intact when only the agent changes', () => {
    const { rerender } = render(element());
    type('half a thought');

    store().selectedAgentId = 'agent-2';
    rerender(element());

    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(store().inputDrafts).toEqual({ __new__: 'half a thought' });
  });

  describe('with an upload in flight', () => {
    it("keeps the completing upload's markdown in the source draft, never the one switched to", () => {
      store().activeSessionId = 'session-a';
      store().inputDrafts['session-b'] = "B's own words";
      const { rerender } = render(element());

      beginUpload('look at this ![](blob:local-preview)');

      store().activeSessionId = 'session-b';
      rerender(element());

      finishUpload('look at this ![](/api/attachments/att-1/download)');
      act(() => {
        vi.advanceTimersByTime(1000);
      });

      expect(store().inputDrafts['session-b']).toBe("B's own words");
      expect(store().inputDrafts['session-a']).toBe(
        'look at this ![](/api/attachments/att-1/download)',
      );
    });

    it('binds an attachment dropped while the editor is still pinned to the source draft', async () => {
      store().activeSessionId = 'session-a';
      mockApiUploadFile.mockImplementation(async () => makeUpload('att-2', 'second.png'));
      const { rerender } = render(element({ uploadEnabled: true }));

      beginUpload('first ![](blob:one)');
      store().activeSessionId = 'session-b';
      rerender(element({ uploadEnabled: true }));

      await act(async () => {
        fireEvent.drop(screen.getByTestId('editor-content'), {
          dataTransfer: { files: [new File(['x'], 'second.png', { type: 'image/png' })] },
        });
        await Promise.resolve();
      });

      const addUpload = (
        useChatStore.getState() as unknown as {
          addInputDraftUpload: ReturnType<typeof vi.fn>;
        }
      ).addInputDraftUpload;
      expect(addUpload).toHaveBeenCalledWith(
        'session-a',
        expect.objectContaining({ status: 'uploading', filename: 'second.png' }),
      );
      expect(addUpload).not.toHaveBeenCalledWith('session-b', expect.anything());
    });

    it("refuses to send while the composer still holds the source draft's document", () => {
      store().activeSessionId = 'session-a';
      const onSend = vi.fn();
      const { rerender } = render(element({ onSend }));

      beginUpload("A's words ![](blob:one)");
      store().activeSessionId = 'session-b';
      rerender(element({ onSend }));

      editorState.uploadingNodes = [];
      act(() => {
        latestEditorOptions.current?.onUpdate?.({ editor: editorInstance.current });
      });

      act(() => {
        capturedExtOptions.current?.onSubmitRef?.current?.();
      });

      expect(onSend).not.toHaveBeenCalled();
    });

    it('loads the target draft once the upload guard clears', () => {
      store().activeSessionId = 'session-a';
      store().inputDrafts['session-b'] = "B's own words";
      const { rerender } = render(element());

      beginUpload('uploading ![](blob:local-preview)');
      store().activeSessionId = 'session-b';
      rerender(element());

      mockSetContent.mockClear();

      finishUpload('uploaded ![](/api/attachments/att-1/download)');
      act(() => {
        vi.advanceTimersByTime(1000);
      });

      expect(mockSetContent).toHaveBeenCalled();
    });
  });

  it('does not strand the last keystrokes when a lazy session create re-keys the draft mid-compose', () => {
    const { rerender } = render(element());
    type('creating a session with this');

    store().activeSessionId = 'session-new';
    rerender(element());

    expect(store().inputDrafts['__new__']).toBe('creating a session with this');
    expect(store().inputDrafts['session-new']).toBeUndefined();
  });
});
