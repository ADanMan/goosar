import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useRef,
  type ReactNode,
  type Ref,
} from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { Attachment, TimelineEntry } from '@goosar/core/types';
import type { UploadResult } from '@goosar/core/hooks/use-file-upload';
import { useCommentDraftStore } from '@goosar/core/issues/stores';
import { renderWithI18n } from '../../test/i18n';

const apiUploadFile = vi.hoisted(() => vi.fn());
const uploadWithToast = vi.hoisted(() => vi.fn());
const editorDefaultValues = vi.hoisted(() => ({
  values: [] as Array<string | undefined>,
}));

let mockUploadIdSeq = 0;

vi.mock('@goosar/core/api', () => ({
  api: { uploadFile: apiUploadFile },
  dispatchReasonCode: () => undefined,
}));

vi.mock('../../navigation', () => ({
  useNavigation: () => ({
    push: vi.fn(),
    pathname: '/acme/issues',
    getShareableUrl: (p: string) => `https://app.example${p}`,
  }),
}));

vi.mock('@goosar/core/workspace/hooks', () => ({
  useActorName: () => ({ getActorName: () => 'Ada' }),
}));

vi.mock('../../common/actor-avatar', () => ({
  ActorAvatar: () => null,
}));

vi.mock('../hooks/use-comment-trigger-preview', () => ({
  useCommentTriggerPreview: () => ({ agents: [], blocked: [] }),
}));

vi.mock('../../editor', async () => ({
  ...(await vi.importActual<typeof import('../../editor/use-upload-gate')>(
    '../../editor/use-upload-gate',
  )),
  ...(await vi.importActual<typeof import('../../editor/use-lazy-editor')>(
    '../../editor/use-lazy-editor',
  )),
  ...(await vi.importActual<typeof import('../../editor/use-composer-submit')>(
    '../../editor/use-composer-submit',
  )),
  useEditorUpload: () => ({ uploadWithToast, upload: vi.fn(), uploading: false }),
  useFileDropZone: () => ({ isDragOver: false, dropZoneProps: {} }),
  FileDropOverlay: () => null,
  ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div>,
  Attachment: () => null,
  AttachmentDownloadProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
  ContentEditor: forwardRef(function MockContentEditor(
    {
      defaultValue,
      onUpdate,
      onUploadFile,
      onUploadingChange,
      onSubmit,
      placeholder,
    }: {
      defaultValue?: string;
      onUpdate?: (markdown: string) => void;
      onUploadFile?: (file: File, uploadId: string) => Promise<UploadResult | null>;
      onUploadingChange?: (uploading: boolean) => void;
      onSubmit?: () => void;
      placeholder?: string;
    },
    ref: Ref<unknown>,
  ) {
    editorDefaultValues.values.push(defaultValue);
    const valueRef = useRef(defaultValue ?? '');
    const inFlightRef = useRef(0);
    useEffect(() => {
      onUploadingChange?.(inFlightRef.current > 0);
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);
    useImperativeHandle(ref, () => ({
      getMarkdown: () => valueRef.current,
      clearContent: () => {
        valueRef.current = '';
      },
      focus: () => {},
      blur: () => {},
      uploadFile: async (file: File) => {
        inFlightRef.current += 1;
        if (inFlightRef.current === 1) onUploadingChange?.(true);
        try {
          const result = await onUploadFile?.(file, `mock-upload-${++mockUploadIdSeq}`);
          if (!result) return;
          valueRef.current = `${valueRef.current}\n${result.url}`.trim();
          onUpdate?.(valueRef.current);
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
      <textarea
        data-testid="editor"
        defaultValue={defaultValue}
        placeholder={placeholder}
        onChange={(e) => {
          valueRef.current = e.target.value;
          onUpdate?.(e.target.value);
        }}
        onKeyDown={(e) => {
          if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') onSubmit?.();
        }}
      />
    );
  }),
}));

import { CommentCard } from './comment-card';

const entry: TimelineEntry = {
  id: 'comment-1',
  issue_id: 'issue-1',
  parent_id: null,
  actor_type: 'member',
  actor_id: 'user-1',
  content: 'Original body',
  type: 'comment',
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
  attachments: [],
  reactions: [],
} as unknown as TimelineEntry;

function renderCard(onEdit = vi.fn().mockResolvedValue(undefined)) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = renderWithI18n(
    <QueryClientProvider client={qc}>
      <CommentCard
        issueId="issue-1"
        entry={entry}
        replies={[]}
        currentUserId="user-1"
        onReply={vi.fn().mockResolvedValue(true)}
        onEdit={onEdit}
        onDelete={vi.fn()}
        onToggleReaction={vi.fn()}
      />
    </QueryClientProvider>,
  );
  return { ...view, onEdit };
}

async function startEditing() {
  const trigger = document.querySelector('button[aria-haspopup="menu"]');
  if (!trigger) throw new Error('Expected the comment actions menu trigger');
  fireEvent.click(trigger);
  fireEvent.click(await screen.findByText('Edit'));
  await screen.findByTestId('editor');
}

function getSaveButton() {
  return screen.getByRole('button', { name: /Save|Uploading…/ });
}

beforeEach(() => {
  uploadWithToast.mockReset();
  apiUploadFile.mockReset();
  useCommentDraftStore.setState({ drafts: {} });
  editorDefaultValues.values = [];
});

describe('comment edit — draft snapshot', () => {
  it('does not feed the persisted edit draft back as a new editor default', async () => {
    renderCard();
    await startEditing();

    fireEvent.change(screen.getByTestId('editor'), {
      target: { value: 'test.de' },
    });

    expect(useCommentDraftStore.getState().getDraft('edit:issue-1:comment-1')).toBe('test.de');
    expect(editorDefaultValues.values.at(-1)).toBe('Original body');
  });
});

describe('comment edit — upload submit gate', () => {
  function startPendingUpload(container: HTMLElement) {
    let resolveUpload!: (att: Attachment) => void;
    apiUploadFile.mockImplementationOnce(
      () =>
        new Promise<Attachment>((resolve) => {
          resolveUpload = resolve;
        }),
    );
    const input = container.querySelector('input[type="file"]');
    if (!input) throw new Error('Expected a file input to render');
    fireEvent.change(input, {
      target: { files: [new File(['x'], 'shot.png', { type: 'image/png' })] },
    });
    return { resolve: (att: Attachment) => resolveUpload(att) };
  }

  it('disables Save while an upload is in flight and re-enables once it settles', async () => {
    const { container } = renderCard();
    await startEditing();
    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'Edited body' } });

    const pending = startPendingUpload(container);

    await waitFor(() => expect(getSaveButton()).toBeDisabled());
    expect(getSaveButton()).toHaveAttribute('aria-busy', 'true');

    await act(async () => {
      pending.resolve({
        id: 'att-1',
        url: 'https://cdn.example/att-1.png',
        download_url: 'https://cdn.example/att-1.png',
        markdown_url: 'https://cdn.example/att-1.png',
        filename: 'shot.png',
        content_type: 'image/png',
        size_bytes: 1,
      } as unknown as Attachment);
    });

    await waitFor(() => expect(getSaveButton()).not.toBeDisabled());
  });

  it('does not stay gated after cancelling edit mid-upload and re-entering', async () => {
    const { container } = renderCard();
    await startEditing();
    fireEvent.change(screen.getByTestId('editor'), { target: { value: 'Edited body' } });

    startPendingUpload(container);
    await waitFor(() => expect(getSaveButton()).toBeDisabled());

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.queryByTestId('editor')).toBeNull());

    await startEditing();
    await waitFor(() => expect(getSaveButton()).not.toBeDisabled());
    expect(getSaveButton()).not.toHaveAttribute('aria-busy');
  });

  it('blocks the Cmd+Enter save path while an upload is in flight', async () => {
    const { container, onEdit } = renderCard();
    await startEditing();
    const editor = screen.getByTestId('editor');
    fireEvent.change(editor, { target: { value: 'Edited body' } });

    startPendingUpload(container);

    fireEvent.keyDown(editor, { key: 'Enter', metaKey: true });
    await Promise.resolve();
    expect(onEdit).not.toHaveBeenCalled();
  });
});
