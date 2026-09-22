import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useRef,
  type ReactNode,
  type Ref,
} from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import type { UploadResult } from '@goosar/core/hooks/use-file-upload';
import type { Attachment } from '@goosar/core/types';
import { useCommentComposerStore, useCommentDraftStore } from '@goosar/core/issues/stores';
import { renderWithI18n } from '../../test/i18n';
import { CommentInput } from './comment-input';
import { ReplyInput } from './reply-input';

const apiUploadFile = vi.hoisted(() => vi.fn());
const uploadWithToast = vi.hoisted(() => vi.fn());
const editorDefaultValues = vi.hoisted(() => ({
  values: [] as Array<string | undefined>,
}));
const focusCalls = vi.hoisted(() => ({ focused: 0, blurred: 0 }));
const insertMarkdownSpy = vi.hoisted(() => vi.fn());
const insertPlaceholderSpy = vi.hoisted(() => vi.fn());
const insertMarkdownBehavior = vi.hoisted(() => ({ succeed: true }));
const editorUploadSignal = vi.hoisted(() => ({
  notify: undefined as ((uploading: boolean) => void) | undefined,
}));

let mockUploadIdSeq = 0;

vi.mock('@goosar/core/api', () => ({
  api: { uploadFile: apiUploadFile },
}));

vi.mock('@goosar/core/hooks/use-file-upload', async () => ({
  ...(await vi.importActual<typeof import('@goosar/core/hooks/use-file-upload')>(
    '@goosar/core/hooks/use-file-upload',
  )),
  useFileUpload: () => ({ uploadWithToast }),
}));

vi.mock('../../common/actor-avatar', () => ({
  ActorAvatar: ({ actorType, actorId }: { actorType: string; actorId: string }) => (
    <span data-testid="actor-avatar">
      {actorType}:{actorId}
    </span>
  ),
}));

vi.mock('../../editor', async () => ({
  ...(await vi.importActual<typeof import('../../editor/use-lazy-editor')>(
    '../../editor/use-lazy-editor',
  )),
  ...(await vi.importActual<typeof import('../../editor/use-upload-gate')>(
    '../../editor/use-upload-gate',
  )),
  ...(await vi.importActual<typeof import('../../editor/use-composer-submit')>(
    '../../editor/use-composer-submit',
  )),
  useEditorUpload: () => ({ uploadWithToast, upload: vi.fn(), uploading: false }),
  useFileDropZone: () => ({
    isDragOver: false,
    dropZoneProps: { 'data-testid': 'drop-zone' },
  }),
  FileDropOverlay: () => null,
  ContentEditor: forwardRef(function MockContentEditor(
    {
      defaultValue,
      onUpdate,
      placeholder,
      onUploadFile,
      onUploadingChange,
      onSubmit,
      onReady,
    }: {
      defaultValue?: string;
      onUpdate?: (markdown: string) => void;
      placeholder?: string;
      onUploadFile?: (file: File, uploadId: string) => Promise<UploadResult | null>;
      onUploadingChange?: (uploading: boolean) => void;
      onSubmit?: () => void;
      onReady?: () => void;
    },
    ref: Ref<unknown>,
  ) {
    editorDefaultValues.values.push(defaultValue);
    editorUploadSignal.notify = onUploadingChange;
    const valueRef = useRef(defaultValue ?? '');
    const inFlightRef = useRef(0);
    const destroyedRef = useRef(false);

    useEffect(() => {
      onReady?.();
      return () => {
        destroyedRef.current = true;
      };
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      clearContent: () => {
        valueRef.current = '';
      },
      focus: () => {
        focusCalls.focused += 1;
      },
      focusAtCoords: () => {},
      blur: () => {
        focusCalls.blurred += 1;
      },
      uploadFile: async (file: File) => {
        inFlightRef.current += 1;
        if (inFlightRef.current === 1) onUploadingChange?.(true);
        try {
          const result = await onUploadFile?.(file, `mock-upload-${++mockUploadIdSeq}`);
          if (!result || destroyedRef.current) return;
          valueRef.current = `${valueRef.current}\n${result.url}`.trim();
          onUpdate?.(valueRef.current);
        } finally {
          inFlightRef.current -= 1;
          if (inFlightRef.current === 0 && !destroyedRef.current) onUploadingChange?.(false);
        }
      },
      hasActiveUploads: () => inFlightRef.current > 0,
      insertUploadPlaceholder: (upload: unknown) => {
        insertPlaceholderSpy(upload);
        return true;
      },
      settleUploadPlaceholder: () => false,
      insertMarkdownAtEnd: (md: string) => {
        insertMarkdownSpy(md);
        if (destroyedRef.current || !insertMarkdownBehavior.succeed) return false;
        valueRef.current = `${valueRef.current}\n\n${md}`.trim();
        onUpdate?.(valueRef.current);
        return true;
      },
    }));

    return (
      <textarea
        data-testid="editor"
        defaultValue={defaultValue}
        placeholder={placeholder}
        onChange={(event) => {
          valueRef.current = event.target.value;
          onUpdate?.(event.target.value);
        }}
        onKeyDown={(event) => {
          if ((event.metaKey || event.ctrlKey) && event.key === 'Enter') onSubmit?.();
        }}
      />
    );
  }),
}));

function renderWithProviders(ui: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });
  return renderWithI18n(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>);
}

function renderCommentInput(onSubmit = vi.fn().mockResolvedValue(true)) {
  const view = renderWithProviders(<CommentInput issueId="issue-1" onSubmit={onSubmit} />);
  return { ...view, onSubmit };
}

function renderReplyInput({
  onSubmit = vi.fn().mockResolvedValue(true),
  size = 'sm',
  draftKey,
}: {
  onSubmit?: (
    content: string,
    attachmentIds?: string[],
    suppressAgentIds?: string[],
  ) => Promise<boolean>;
  size?: 'sm' | 'default';
  draftKey?: `reply:${string}:${string}`;
} = {}) {
  const view = renderWithProviders(
    <ReplyInput
      issueId="issue-1"
      parentId="comment-1"
      avatarType="member"
      avatarId="user-1"
      onSubmit={onSubmit}
      size={size}
      draftKey={draftKey}
    />,
  );
  return { ...view, onSubmit };
}

function activateComposer(shellTestId: 'comment-composer-shell' | 'reply-composer-shell') {
  fireEvent.click(screen.getByTestId(shellTestId));
}

function getSubmitButton(container: HTMLElement): HTMLButtonElement {
  const buttons = container.querySelectorAll('button');
  const button = buttons[buttons.length - 1];
  if (!button) throw new Error('Expected submit button to render');
  return button;
}

beforeEach(() => {
  uploadWithToast.mockReset();
  apiUploadFile.mockReset();
  insertMarkdownSpy.mockReset();
  insertPlaceholderSpy.mockReset();
  insertMarkdownBehavior.succeed = true;
  localStorage.clear();
  useCommentComposerStore.setState({ sticky: true });
  useCommentDraftStore.setState({ drafts: {} });
  editorDefaultValues.values = [];
  focusCalls.focused = 0;
  focusCalls.blurred = 0;
});

describe('comment composers', () => {
  it('renders the main comment composer without a manual expand control', () => {
    const { container } = renderCommentInput();

    expect(screen.getByTestId('comment-composer-shell')).toHaveTextContent('Leave a comment...');
    activateComposer('comment-composer-shell');
    expect(screen.getByPlaceholderText('Leave a comment...')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Attach file' })).toBeInTheDocument();
    expect(container.querySelectorAll('button')).toHaveLength(2);

    const shell = screen.getByTestId('drop-zone');
    expect(shell.className).not.toMatch(/max-h-/);
    expect(shell.className).not.toContain('h-[70vh]');
  });

  it('renders reply composer without a manual expand control', () => {
    const { container } = renderReplyInput();

    expect(screen.getByTestId('reply-composer-shell')).toHaveTextContent('Leave a reply...');
    activateComposer('reply-composer-shell');
    expect(screen.getByPlaceholderText('Leave a reply...')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Attach file' })).toBeInTheDocument();
    expect(container.querySelectorAll('button')).toHaveLength(2);

    const shell = screen.getByTestId('drop-zone');
    expect(shell.className).not.toMatch(/max-h-/);
    expect(shell.className).not.toContain('h-[60vh]');
  });

  it('lets default-size replies grow without a height cap', () => {
    const { container } = renderReplyInput({ size: 'default' });

    activateComposer('reply-composer-shell');
    expect(screen.getByPlaceholderText('Leave a reply...')).toBeInTheDocument();
    expect(container.querySelectorAll('button')).toHaveLength(2);

    const shell = screen.getByTestId('drop-zone');
    expect(shell.className).not.toMatch(/max-h-/);
  });

  it('keeps main comment submission wired after removing expand', async () => {
    const { container, onSubmit } = renderCommentInput();

    activateComposer('comment-composer-shell');
    fireEvent.change(screen.getByTestId('editor'), {
      target: { value: 'hello from composer' },
    });
    fireEvent.click(getSubmitButton(container));

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith('hello from composer', undefined, undefined);
    });
  });

  it('keeps reply submission wired after removing expand', async () => {
    const { container, onSubmit } = renderReplyInput();

    activateComposer('reply-composer-shell');
    fireEvent.change(screen.getByTestId('editor'), {
      target: { value: 'thread reply' },
    });
    fireEvent.click(getSubmitButton(container));

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith('thread reply', undefined, undefined);
    });
  });

  it("keeps the main comment editor's initial draft snapshot after persistence rerenders", () => {
    renderCommentInput();
    activateComposer('comment-composer-shell');

    fireEvent.change(screen.getByTestId('editor'), {
      target: { value: 'test.de' },
    });

    expect(useCommentDraftStore.getState().getDraft('new:issue-1')).toBe('test.de');
    expect(editorDefaultValues.values.at(-1)).toBeUndefined();
  });

  it("keeps the reply editor's initial draft snapshot after persistence rerenders", () => {
    renderReplyInput({ draftKey: 'reply:issue-1:comment-1' });
    activateComposer('reply-composer-shell');

    fireEvent.change(screen.getByTestId('editor'), {
      target: { value: 'test.de' },
    });

    expect(useCommentDraftStore.getState().getDraft('reply:issue-1:comment-1')).toBe('test.de');
    expect(editorDefaultValues.values.at(-1)).toBeUndefined();
  });

  it('locks the editor while the send is in flight, then clears on success', async () => {
    let resolveSubmit: (ok: boolean) => void = () => {};
    const onSubmit = vi.fn(
      () =>
        new Promise<boolean>((resolve) => {
          resolveSubmit = resolve;
        }),
    );
    const { container } = renderCommentInput(onSubmit);

    activateComposer('comment-composer-shell');
    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'sending' } });
    fireEvent.click(getSubmitButton(container));

    await waitFor(() =>
      expect(screen.getByTestId('editor').closest('[aria-busy]')).toHaveAttribute(
        'aria-busy',
        'true',
      ),
    );
    expect(onSubmit).toHaveBeenCalledWith('sending', undefined, undefined);

    resolveSubmit(true);

    await waitFor(() => expect(getSubmitButton(container)).toBeDisabled());
    expect(screen.getByTestId('editor').closest('[aria-busy]')).toBeNull();
  });

  it('blurs the top-level composer after a posted comment', async () => {
    const { container } = renderCommentInput();

    activateComposer('comment-composer-shell');
    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'posted' } });
    focusCalls.focused = 0;
    fireEvent.click(getSubmitButton(container));

    await waitFor(() => expect(focusCalls.blurred).toBeGreaterThan(0));
    expect(focusCalls.focused).toBe(0);
  });

  it('keeps the caret in the reply box after a posted reply', async () => {
    const { container } = renderReplyInput();

    activateComposer('reply-composer-shell');
    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'replied' } });
    focusCalls.focused = 0;
    fireEvent.click(getSubmitButton(container));

    await waitFor(() => expect(focusCalls.focused).toBeGreaterThan(0));
    expect(focusCalls.blurred).toBe(0);
  });

  it('does not refocus the reply box when the send fails', async () => {
    const onSubmit = vi.fn().mockResolvedValue(false);
    const { container } = renderReplyInput({ onSubmit });

    activateComposer('reply-composer-shell');
    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'nope' } });
    focusCalls.focused = 0;
    fireEvent.click(getSubmitButton(container));

    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
    await new Promise((resolve) => requestAnimationFrame(() => resolve(null)));
    expect(focusCalls.focused).toBe(0);
  });

  it('a tab switch during the send does not resurrect the posted draft', async () => {
    let resolveSubmit!: (v: boolean) => void;
    const onSubmit = vi.fn(
      () =>
        new Promise<boolean>((r) => {
          resolveSubmit = r;
        }),
    );
    renderCommentInput(onSubmit);
    activateComposer('comment-composer-shell');
    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'draft A' } });
    fireEvent.keyDown(screen.getByTestId('editor'), { key: 'Enter', metaKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());

    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      get: () => 'hidden',
    });
    fireEvent(document, new Event('visibilitychange'));

    await act(async () => {
      resolveSubmit(true);
      await Promise.resolve();
    });

    expect(useCommentDraftStore.getState().getDraft('new:issue-1')).toBeUndefined();

    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      get: () => 'visible',
    });
  });

  it('text typed while a comment send is in flight survives the success', async () => {
    let resolveSubmit!: (v: boolean) => void;
    const onSubmit = vi.fn(
      () =>
        new Promise<boolean>((r) => {
          resolveSubmit = r;
        }),
    );
    renderCommentInput(onSubmit);
    activateComposer('comment-composer-shell');
    const editor = screen.getByTestId('editor');
    fireEvent.change(editor, { target: { value: 'draft A' } });
    fireEvent.keyDown(editor, { key: 'Enter', metaKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());

    fireEvent.change(editor, { target: { value: 'draft A plus more' } });

    await act(async () => {
      resolveSubmit(true);
      await Promise.resolve();
    });

    expect(useCommentDraftStore.getState().getDraft('new:issue-1')).toBe('draft A plus more');
    await new Promise((resolve) => requestAnimationFrame(() => resolve(null)));
    expect(focusCalls.blurred).toBe(0);
  });

  it('does not refocus a reply box whose mid-flight draft was kept', async () => {
    let resolveSubmit!: (v: boolean) => void;
    const onSubmit = vi.fn(
      () =>
        new Promise<boolean>((r) => {
          resolveSubmit = r;
        }),
    );
    renderReplyInput({ onSubmit, draftKey: 'reply:issue-1:comment-1' });
    activateComposer('reply-composer-shell');
    const editor = screen.getByTestId('editor');
    fireEvent.change(editor, { target: { value: 'reply A' } });
    fireEvent.keyDown(editor, { key: 'Enter', metaKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());

    fireEvent.change(editor, { target: { value: 'reply A plus more' } });
    focusCalls.focused = 0;

    await act(async () => {
      resolveSubmit(true);
      await Promise.resolve();
    });

    expect(useCommentDraftStore.getState().getDraft('reply:issue-1:comment-1')).toBe(
      'reply A plus more',
    );
    await new Promise((resolve) => requestAnimationFrame(() => resolve(null)));
    expect(focusCalls.focused).toBe(0);
  });

  it('a late comment success does NOT clear a draft typed after unmount', async () => {
    let resolveSubmit!: (v: boolean) => void;
    const onSubmit = vi.fn(
      () =>
        new Promise<boolean>((r) => {
          resolveSubmit = r;
        }),
    );
    const view = renderCommentInput(onSubmit);
    activateComposer('comment-composer-shell');
    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'draft A' } });
    fireEvent.keyDown(screen.getByTestId('editor'), { key: 'Enter', metaKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());

    view.unmount();
    useCommentDraftStore.getState().setDraft('new:issue-1', 'draft B');

    await act(async () => {
      resolveSubmit(true);
      await Promise.resolve();
    });

    expect(useCommentDraftStore.getState().getDraft('new:issue-1')).toBe('draft B');
  });

  it('a late comment success still clears an untouched draft', async () => {
    let resolveSubmit!: (v: boolean) => void;
    const onSubmit = vi.fn(
      () =>
        new Promise<boolean>((r) => {
          resolveSubmit = r;
        }),
    );
    const view = renderCommentInput(onSubmit);
    activateComposer('comment-composer-shell');
    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'draft A' } });
    fireEvent.keyDown(screen.getByTestId('editor'), { key: 'Enter', metaKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());

    view.unmount();
    await act(async () => {
      resolveSubmit(true);
      await Promise.resolve();
    });

    expect(useCommentDraftStore.getState().getDraft('new:issue-1')).toBeUndefined();
  });

  it('keeps the draft when the send fails (no optimistic clear)', async () => {
    const onSubmit = vi.fn().mockResolvedValue(false);
    const { container } = renderCommentInput(onSubmit);

    activateComposer('comment-composer-shell');
    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'will fail' } });
    fireEvent.click(getSubmitButton(container));

    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
    await waitFor(() => expect(getSubmitButton(container)).not.toBeDisabled());
  });
});

describe('comment composers — upload submit gate', () => {
  const uploadAttachment = (id: string, url: string) =>
    ({
      id,
      url,
      download_url: url,
      markdown_url: url,
      filename: `${id}.png`,
      content_type: 'image/png',
      size_bytes: 1,
    }) as unknown as Attachment;

  function startPendingUpload(container: HTMLElement, filename = 'slow.png') {
    let resolveUpload!: (att: Attachment) => void;
    let rejectUpload!: (err: Error) => void;
    apiUploadFile.mockImplementationOnce(
      () =>
        new Promise<Attachment>((resolve, reject) => {
          resolveUpload = resolve;
          rejectUpload = reject;
        }),
    );
    const input = container.querySelector('input[type="file"]');
    if (!input) throw new Error('Expected a file input to render');
    fireEvent.change(input, {
      target: { files: [new File(['x'], filename, { type: 'image/png' })] },
    });
    return {
      resolve: (att: Attachment) => resolveUpload(att),
      fail: () => rejectUpload(new Error('upload failed')),
    };
  }

  it('disables send while an upload is in flight and re-enables once it settles', async () => {
    const { container } = renderCommentInput();
    activateComposer('comment-composer-shell');
    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'look at this' } });

    const pending = startPendingUpload(container);

    await waitFor(() => expect(getSubmitButton(container)).toBeDisabled());
    expect(getSubmitButton(container)).toHaveAttribute('aria-busy', 'true');

    await act(async () => {
      pending.resolve(uploadAttachment('att-1', 'https://cdn.example/att-1.png'));
    });

    await waitFor(() => expect(getSubmitButton(container)).not.toBeDisabled());
    expect(getSubmitButton(container)).not.toHaveAttribute('aria-busy');
  });

  it('never asks the rebuild to draw an upload this mount started', async () => {
    const { container } = renderCommentInput();
    activateComposer('comment-composer-shell');
    const pending = startPendingUpload(container, 'mine.png');

    await waitFor(() => expect(getSubmitButton(container)).toBeDisabled());
    expect(insertPlaceholderSpy).not.toHaveBeenCalled();

    await act(async () => {
      pending.resolve(uploadAttachment('att-mine', 'https://cdn.example/att-mine.png'));
    });
    expect(insertPlaceholderSpy).not.toHaveBeenCalled();
  });

  it('does not redraw a placeholder the user deleted mid-upload', async () => {
    useCommentDraftStore.getState().addUpload('new:issue-1', {
      clientUploadId: 'u-deleted',
      status: 'uploading',
      filename: 'gone.png',
      size: 1,
    });
    renderCommentInput();
    await screen.findByTestId('editor');
    await waitFor(() => expect(insertPlaceholderSpy).toHaveBeenCalledTimes(1));

    act(() => {
      useCommentDraftStore.getState().addUpload('new:issue-1', {
        clientUploadId: 'u-second',
        status: 'uploading',
        filename: 'other.png',
        size: 1,
      });
    });

    await waitFor(() =>
      expect(insertPlaceholderSpy.mock.calls.some(([u]) => u.uploadId === 'u-second')).toBe(true),
    );
    expect(
      insertPlaceholderSpy.mock.calls.filter(([u]) => u.uploadId === 'u-deleted'),
    ).toHaveLength(1);
  });

  it('leaves nothing behind when an upload fails', async () => {
    const { container } = renderCommentInput();
    activateComposer('comment-composer-shell');

    const pending = startPendingUpload(container, 'doomed.png');
    await waitFor(() => expect(getSubmitButton(container)).toBeDisabled());

    await act(async () => {
      pending.fail();
    });

    await waitFor(() =>
      expect(useCommentDraftStore.getState().getUploads('new:issue-1')).toHaveLength(0),
    );
    expect(screen.queryByText(/doomed\.png/)).toBeNull();
    expect(useCommentDraftStore.getState().getDraft('new:issue-1')).toBeFalsy();
  });

  it('rebuilds a placeholder for an upload inherited from the persisted draft', async () => {
    useCommentDraftStore.getState().addUpload('new:issue-1', {
      clientUploadId: 'from-a-previous-mount',
      status: 'uploading',
      filename: 'orphan.png',
      size: 1,
    });

    renderCommentInput();
    await screen.findByTestId('editor');

    await waitFor(() =>
      expect(insertPlaceholderSpy).toHaveBeenCalledWith(
        expect.objectContaining({ uploadId: 'from-a-previous-mount', filename: 'orphan.png' }),
      ),
    );
  });

  it('blocks the Cmd+Enter path while an upload is in flight', async () => {
    const { container, onSubmit } = renderCommentInput();
    activateComposer('comment-composer-shell');
    const editor = screen.getByTestId('editor');
    fireEvent.change(editor, { target: { value: 'look at this' } });

    const pending = startPendingUpload(container);

    fireEvent.keyDown(editor, { key: 'Enter', metaKey: true });
    await Promise.resolve();
    expect(onSubmit).not.toHaveBeenCalled();

    await act(async () => {
      pending.resolve(uploadAttachment('att-1', 'https://cdn.example/att-1.png'));
    });

    fireEvent.keyDown(editor, { key: 'Enter', metaKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
  });

  it('blocks send while a coordinator upload from a PREVIOUS mount is still in flight', async () => {
    useCommentDraftStore.getState().setDraft('new:issue-1', 'recovered draft');
    useCommentDraftStore.getState().addUpload('new:issue-1', {
      clientUploadId: 'prev-mount-upload',
      status: 'uploading',
      filename: 'shot.png',
      size: 10,
    });

    const { container, onSubmit } = renderCommentInput();
    const editor = await screen.findByTestId('editor');

    expect(getSubmitButton(container)).toBeDisabled();
    expect(getSubmitButton(container)).toHaveAttribute('aria-busy', 'true');

    fireEvent.keyDown(editor, { key: 'Enter', metaKey: true });
    await Promise.resolve();
    expect(onSubmit).not.toHaveBeenCalled();

    await act(async () => {
      useCommentDraftStore
        .getState()
        .settleUpload(
          'new:issue-1',
          'prev-mount-upload',
          uploadAttachment('att-prev', 'https://cdn.example/att-prev.png'),
        );
    });
    await waitFor(() => expect(getSubmitButton(container)).not.toBeDisabled());
    fireEvent.keyDown(editor, { key: 'Enter', metaKey: true });
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
  });

  it('stays gated until the LAST of two concurrent uploads settles', async () => {
    const { container } = renderCommentInput();
    activateComposer('comment-composer-shell');
    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'two files' } });

    const first = startPendingUpload(container, 'a.png');
    const second = startPendingUpload(container, 'b.png');

    await waitFor(() => expect(getSubmitButton(container)).toBeDisabled());

    await act(async () => {
      first.resolve(uploadAttachment('att-a', 'https://cdn.example/att-a.png'));
    });
    expect(getSubmitButton(container)).toBeDisabled();

    await act(async () => {
      second.resolve(uploadAttachment('att-b', 'https://cdn.example/att-b.png'));
    });
    await waitFor(() => expect(getSubmitButton(container)).not.toBeDisabled());
  });

  it("re-enables send after a FAILED upload so the draft isn't stuck", async () => {
    const { container } = renderCommentInput();
    activateComposer('comment-composer-shell');
    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'will fail' } });

    const pending = startPendingUpload(container);
    await waitFor(() => expect(getSubmitButton(container)).toBeDisabled());

    await act(async () => {
      pending.fail();
    });
    await waitFor(() => expect(getSubmitButton(container)).not.toBeDisabled());
  });

  it('sends the attachment id once the upload completes', async () => {
    const { container, onSubmit } = renderCommentInput();
    activateComposer('comment-composer-shell');
    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'shipping it' } });

    const pending = startPendingUpload(container);
    await act(async () => {
      pending.resolve(uploadAttachment('att-9', 'https://cdn.example/att-9.png'));
    });

    await waitFor(() => expect(getSubmitButton(container)).not.toBeDisabled());
    fireEvent.click(getSubmitButton(container));

    await waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith(
        expect.stringContaining('https://cdn.example/att-9.png'),
        ['att-9'],
        undefined,
      ),
    );
  });

  it('does NOT bind an upload the user deleted from the body', async () => {
    const { container, onSubmit } = renderCommentInput();
    activateComposer('comment-composer-shell');
    const editor = screen.getByTestId('editor');
    fireEvent.change(editor, { target: { value: 'keep this' } });

    const pending = startPendingUpload(container);
    await act(async () => {
      pending.resolve(uploadAttachment('att-del', 'https://cdn.example/att-del.png'));
    });

    fireEvent.change(editor, { target: { value: 'keep this, dropped the image' } });
    fireEvent.keyDown(editor, { key: 'Enter', metaKey: true });

    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
    expect(onSubmit).toHaveBeenCalledWith('keep this, dropped the image', undefined, undefined);
  });

  it("writes the finished upload's link into the persisted draft after the composer unmounts", async () => {
    const { container, unmount } = renderCommentInput();
    activateComposer('comment-composer-shell');
    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'wip text' } });

    const pending = startPendingUpload(container);
    unmount();

    await act(async () => {
      pending.resolve(uploadAttachment('att-wb', 'https://cdn.example/att-wb.png'));
    });

    const draft = useCommentDraftStore.getState().getDraft('new:issue-1');
    expect(draft).toContain('wip text');
    expect(draft).toContain('https://cdn.example/att-wb.png');
    const uploads = useCommentDraftStore.getState().getUploads('new:issue-1');
    expect(uploads).toHaveLength(1);
    expect(uploads[0]?.status).toBe('uploaded');
  });

  it("hands the finished upload's link to a REOPENED composer's live editor", async () => {
    const first = renderCommentInput();
    activateComposer('comment-composer-shell');
    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'draft body' } });

    const pending = startPendingUpload(first.container);
    first.unmount();

    renderCommentInput();
    await screen.findByTestId('editor');

    await act(async () => {
      pending.resolve(uploadAttachment('att-live', 'https://cdn.example/att-live.png'));
    });

    expect(insertMarkdownSpy).toHaveBeenCalledWith(
      expect.stringContaining('https://cdn.example/att-live.png'),
    );
    await waitFor(() =>
      expect(useCommentDraftStore.getState().getDraft('new:issue-1')).toContain(
        'https://cdn.example/att-live.png',
      ),
    );
  });

  it("retries the insert while the reopened editor's instance is still warming up", async () => {
    const first = renderCommentInput();
    activateComposer('comment-composer-shell');
    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'draft body' } });

    const pending = startPendingUpload(first.container);
    first.unmount();

    renderCommentInput();
    await screen.findByTestId('editor');

    insertMarkdownBehavior.succeed = false;
    await act(async () => {
      pending.resolve(uploadAttachment('att-retry', 'https://cdn.example/att-retry.png'));
    });

    expect(useCommentDraftStore.getState().getDraft('new:issue-1') ?? '').not.toContain(
      'att-retry.png',
    );

    insertMarkdownBehavior.succeed = true;
    await waitFor(
      () =>
        expect(useCommentDraftStore.getState().getDraft('new:issue-1')).toContain(
          'https://cdn.example/att-retry.png',
        ),
      { timeout: 3000 },
    );
  });

  it("gates the reply composer's send button too", async () => {
    const { container } = renderReplyInput();
    activateComposer('reply-composer-shell');
    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'replying' } });

    startPendingUpload(container);

    await waitFor(() => expect(getSubmitButton(container)).toBeDisabled());
    expect(getSubmitButton(container)).toHaveAttribute('aria-busy', 'true');
  });

  it("blocks the reply composer's Cmd+Enter path", async () => {
    const onSubmit = vi.fn().mockResolvedValue(true);
    const { container } = renderReplyInput({ onSubmit });
    activateComposer('reply-composer-shell');
    const editor = screen.getByTestId('editor');
    fireEvent.change(editor, { target: { value: 'replying' } });

    startPendingUpload(container);

    fireEvent.keyDown(editor, { key: 'Enter', metaKey: true });
    await Promise.resolve();
    expect(onSubmit).not.toHaveBeenCalled();
  });
});

describe('sticky composer preference', () => {
  it('caps the editor height while the sticky preference is on (default)', () => {
    renderCommentInput();

    activateComposer('comment-composer-shell');
    expect(screen.getByTestId('editor').parentElement?.className).toContain('max-h-[40vh]');
  });

  it('lets the editor grow when the preference is off', () => {
    useCommentComposerStore.setState({ sticky: false });
    renderCommentInput();

    activateComposer('comment-composer-shell');
    expect(screen.getByTestId('editor').parentElement?.className).not.toContain('max-h-[40vh]');
  });
});
