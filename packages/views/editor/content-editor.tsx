'use client';

import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
} from 'react';
import { useEditor, EditorContent, type Editor } from '@tiptap/react';
import { cn } from '@goosar/ui/lib/utils';
import type { UploadResult } from '@goosar/core/hooks/use-file-upload';
import { useWorkspaceSlug } from '@goosar/core/paths';
import { useQueryClient } from '@tanstack/react-query';
import { issueIdentifierOptions } from '@goosar/core/issues/queries';
import { workspaceListOptions } from '@goosar/core/workspace/queries';
import { isIssueIdentifier } from '@goosar/ui/markdown';
import type { Attachment } from '@goosar/core/types';
import {
  parseMarkdownChunked,
  MARKDOWN_CHUNK_THRESHOLD,
  type MarkdownManagerLike,
} from './utils/parse-markdown-chunked';
import type { MentionItem } from './extensions/mention-suggestion';
import type { IssueIdentifierResolver } from './extensions/issue-identifier-autolink';
import { createEditorExtensions } from './extensions';
import {
  uploadAndInsertFile,
  insertUploadPlaceholder,
  settleUploadNode,
} from './extensions/file-upload';
import { configStore } from '@goosar/core/config';
import { preprocessMarkdown } from './utils/preprocess';
import { repairEmptyListItems } from './utils/repair-list-items';
import { useAppOrigin } from '../navigation';
import { openLink, isMentionHref } from './utils/link-handler';
import { EditorBubbleMenu } from './bubble-menu';
import { posFromAnchor, type TextAnchor } from './text-anchor';
import { useLinkHover, LinkHoverCard } from './link-hover-card';
import { AttachmentDownloadProvider } from './attachment-download-context';
import 'katex/dist/katex.min.css';
import './styles/index.css';

function normalizeMarkdown(md: string): string {
  return md.trim();
}

function normalizeEditorMarkdown(editor: Editor): string {
  return normalizeMarkdown(editor.getMarkdown());
}

function hasUploadingNode(editor: Editor): boolean {
  let found = false;
  editor.state.doc.descendants((node) => {
    if (node.attrs.uploading) found = true;
    return !found;
  });
  return found;
}

interface ContentEditorBaseProps {
  onUpdate?: (markdown: string) => void;
  placeholder?: string;
  className?: string;
  debounceMs?: number;
  onSubmit?: () => void;
  onBlur?: () => void;
  onUploadFile?: (file: File, uploadId: string) => Promise<UploadResult | null>;
  pasteAsFileThreshold?: number;
  onUploadingChange?: (uploading: boolean) => void;
  showBubbleMenu?: boolean;
  currentIssueId?: string;
  disableMentions?: boolean;
  mentionMode?: 'default' | 'context';
  mentionContextItems?: MentionItem[];
  enableSlashCommands?: boolean;
  slashCommandMode?: 'skill' | 'command';
  attachments?: Attachment[];
  flushPendingOnUnmount?: boolean;
  onReady?: () => void;
}

type ContentEditorValueProps =
  | {
      defaultValue?: string;
      value?: never;
    }
  | {
      value: string;
      defaultValue?: never;
    };

type ContentEditorProps = ContentEditorBaseProps & ContentEditorValueProps;

interface ContentEditorRef {
  getMarkdown: () => string;
  clearContent: () => void;
  focus: () => void;
  focusAtCoords: (coords: { x: number; y: number }) => void;
  focusAtAnchor: (anchor: TextAnchor) => void;
  blur: () => void;
  uploadFile: (file: File) => void;
  hasActiveUploads: () => boolean;
  insertMarkdownAtEnd: (markdown: string) => boolean;
  insertUploadPlaceholder: (upload: {
    uploadId: string;
    filename: string;
    size?: number;
  }) => boolean;
  settleUploadPlaceholder: (uploadId: string, result: UploadResult) => boolean;
  flushPendingUpdate: () => string | null;
  adoptContent: (markdown: string) => void;
}

const ContentEditor = forwardRef<ContentEditorRef, ContentEditorProps>(function ContentEditor(
  {
    defaultValue,
    value,
    onUpdate,
    placeholder: placeholderText = '',
    className,
    debounceMs = 300,
    onSubmit,
    onBlur,
    onUploadFile,
    pasteAsFileThreshold,
    onUploadingChange,
    showBubbleMenu = true,
    currentIssueId,
    disableMentions = false,
    mentionMode = 'default',
    mentionContextItems,
    enableSlashCommands = false,
    slashCommandMode = 'skill',
    attachments,
    flushPendingOnUnmount = false,
    onReady,
  },
  ref,
) {
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  const flushPendingOnUnmountRef = useRef(flushPendingOnUnmount);
  const pendingFlushRef = useRef<string | null>(null);
  const onUpdateRef = useRef(onUpdate);
  const onSubmitRef = useRef(onSubmit);
  const onBlurRef = useRef(onBlur);
  const onReadyRef = useRef(onReady);
  const onUploadingChangeRef = useRef(onUploadingChange);
  const onUploadFileRef = useRef<
    ((file: File, uploadId: string) => Promise<UploadResult | null>) | undefined
  >(undefined);
  const pasteAsFileThresholdRef = useRef<number | undefined>(pasteAsFileThreshold);
  const mentionContextItemsRef = useRef<MentionItem[]>(mentionContextItems ?? []);
  const lastEmittedRef = useRef<string | null>(null);
  const lastSyncedValueRef = useRef(value);
  const placeholderRef = useRef(placeholderText);

  const [sessionUploads, setSessionUploads] = useState<Attachment[]>([]);
  const wrappedOnUploadFile = useMemo(() => {
    if (!onUploadFile) return undefined;
    return async (file: File, uploadId: string): Promise<UploadResult | null> => {
      const result = await onUploadFile(file, uploadId);
      if (result?.id) {
        setSessionUploads((prev) =>
          prev.some((a) => a.id === result.id) ? prev : [...prev, result],
        );
      }
      return result;
    };
  }, [onUploadFile]);

  const providerAttachments = useMemo(() => {
    if (sessionUploads.length === 0) return attachments;
    const sessionById = new Map(sessionUploads.map((a) => [a.id, a]));
    const merged: Attachment[] = [];
    for (const a of attachments ?? []) {
      const session = a.id ? sessionById.get(a.id) : undefined;
      if (session) sessionById.delete(a.id);
      merged.push(session && !a.download_url ? { ...a, download_url: session.download_url } : a);
    }
    merged.push(...sessionById.values());
    return merged;
  }, [attachments, sessionUploads]);

  const workspaceSlug = useWorkspaceSlug();
  const workspaceSlugRef = useRef(workspaceSlug);
  workspaceSlugRef.current = workspaceSlug;

  const appOrigin = useAppOrigin();
  const appOriginRef = useRef(appOrigin);
  appOriginRef.current = appOrigin;

  onUpdateRef.current = onUpdate;
  onSubmitRef.current = onSubmit;
  onBlurRef.current = onBlur;
  onReadyRef.current = onReady;
  onUploadingChangeRef.current = onUploadingChange;
  onUploadFileRef.current = wrappedOnUploadFile;
  pasteAsFileThresholdRef.current = pasteAsFileThreshold;
  mentionContextItemsRef.current = mentionContextItems ?? [];
  flushPendingOnUnmountRef.current = flushPendingOnUnmount;

  const queryClient = useQueryClient();

  const resolveIssueIdentifierRef = useRef<IssueIdentifierResolver | undefined>(undefined);
  resolveIssueIdentifierRef.current = async (identifier) => {
    if (!isIssueIdentifier(identifier)) return null;
    const slug = workspaceSlugRef.current;
    if (!slug) return null;
    const workspaces = await queryClient.fetchQuery(workspaceListOptions());
    const ws = workspaces.find((w) => w.slug === slug);
    if (!ws) return null;
    const prefix = ws.issue_prefix;
    if (prefix && !identifier.toUpperCase().startsWith(`${prefix.toUpperCase()}-`)) {
      return null;
    }
    const issue = await queryClient.fetchQuery(issueIdentifierOptions(ws.id, identifier));
    return issue ? { id: issue.id, identifier: issue.identifier } : null;
  };

  const initialMarkdown = value ?? defaultValue ?? '';
  const initialContent = initialMarkdown
    ? 
      preprocessMarkdown(initialMarkdown, {
        cdnDomain: configStore.getState().cdnDomain,
      })
    : '';
  const focusOnReadyRef = useRef(false);
  const mountChunked = initialContent.length > MARKDOWN_CHUNK_THRESHOLD;

  const editor = useEditor({
    immediatelyRender: false,
    shouldRerenderOnTransaction: false,
    onCreate: ({ editor: ed }) => {
      if (mountChunked) {
        const manager = (ed.storage as { markdown?: { manager?: MarkdownManagerLike } }).markdown
          ?.manager;
        if (manager) {
          ed.commands.setContent(parseMarkdownChunked(manager, initialContent), {
            emitUpdate: false,
          });
        } else {
          ed.commands.setContent(initialContent, {
            emitUpdate: false,
            contentType: 'markdown',
          });
        }
      }
      repairEmptyListItems(ed);
      lastEmittedRef.current = normalizeEditorMarkdown(ed);
      if (focusOnReadyRef.current) {
        focusOnReadyRef.current = false;
        ed.commands.focus('end');
      }
    },
    content: mountChunked ? '' : initialContent,
    contentType: mountChunked ? undefined : initialMarkdown ? 'markdown' : undefined,
    extensions: createEditorExtensions({
      placeholder: () => placeholderRef.current,
      queryClient,
      onSubmitRef,
      onUploadFileRef,
      pasteAsFileThresholdRef,
      disableMentions,
      mentionMode,
      getMentionContextItems: () => mentionContextItemsRef.current,
      enableSlashCommands,
      slashCommandMode,
      resolveIssueIdentifierRef,
    }),
    onUpdate: ({ editor: ed }) => {
      if (!onUpdateRef.current) return;
      if (flushPendingOnUnmountRef.current) {
        pendingFlushRef.current = normalizeEditorMarkdown(ed);
      }
      if (debounceRef.current) clearTimeout(debounceRef.current);
      debounceRef.current = setTimeout(() => {
        debounceRef.current = undefined;
        pendingFlushRef.current = null;
        const md = normalizeEditorMarkdown(ed);
        if (md === lastEmittedRef.current) return;
        lastEmittedRef.current = md;
        onUpdateRef.current?.(md);
      }, debounceMs);
    },
    onBlur: () => {
      onBlurRef.current?.();
    },
    editorProps: {
      handleDOMEvents: {
        click(_view, event) {
          const target = event.target as HTMLElement;
          if (target.closest('[data-node-view-wrapper]')) return false;

          const link = target.closest('a');
          const href = link?.getAttribute('href');
          if (!href || isMentionHref(href)) return false;

          event.preventDefault();
          openLink(href, workspaceSlugRef.current, appOriginRef.current);
          return true;
        },
      },
      attributes: {
        class: cn('flex-1 rich-text-editor text-sm outline-none', className),
      },
    },
  });

  const readyFiredRef = useRef(false);
  useEffect(() => {
    if (!editor || readyFiredRef.current) return;
    readyFiredRef.current = true;
    onReadyRef.current?.();
  }, [editor]);

  useEffect(() => {
    if (!editor || !onUploadingChange) return;
    let last = hasUploadingNode(editor);
    onUploadingChangeRef.current?.(last);
    const check = () => {
      if (editor.isDestroyed) return;
      const uploading = hasUploadingNode(editor);
      if (uploading === last) return;
      last = uploading;
      onUploadingChangeRef.current?.(uploading);
    };
    editor.on('transaction', check);
    return () => {
      editor.off('transaction', check);
    };
    // `onUploadingChange` is read for presence only; the ref carries the
    // live callback, so a host passing an inline arrow doesn't rebind.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editor, !!onUploadingChange]);

  useEffect(() => {
    return () => {
      if (!debounceRef.current) return;
      clearTimeout(debounceRef.current);
      debounceRef.current = undefined;
      if (!flushPendingOnUnmountRef.current) return;
      const pending = pendingFlushRef.current;
      pendingFlushRef.current = null;
      if (pending === null || pending === lastEmittedRef.current) return;
      lastEmittedRef.current = pending;
      onUpdateRef.current?.(pending);
    };
  }, []);

  const applyExternalContent = useCallback(
    (markdown: string) => {
      if (!editor || editor.isDestroyed) return;
      const before = normalizeEditorMarkdown(editor);

      if (normalizeMarkdown(markdown) === before) return;

      const incoming = markdown
        ? preprocessMarkdown(markdown, {
            cdnDomain: configStore.getState().cdnDomain,
          })
        : '';
      const incomingNormalized = normalizeMarkdown(incoming);
      if (incomingNormalized === before) return;

      const { from, to } = editor.state.selection;
      const manager =
        incoming.length > MARKDOWN_CHUNK_THRESHOLD
          ? (editor.storage as { markdown?: { manager?: MarkdownManagerLike } }).markdown?.manager
          : undefined;
      if (manager) {
        editor.commands.setContent(parseMarkdownChunked(manager, incoming), {
          emitUpdate: false,
        });
      } else {
        editor.commands.setContent(incoming, {
          emitUpdate: false,
          contentType: 'markdown',
        });
      }

      if (!repairEmptyListItems(editor, { from, to })) {
        const docSize = editor.state.doc.content.size;
        editor.commands.setTextSelection({
          from: Math.min(from, docSize),
          to: Math.min(to, docSize),
        });
      }

      lastEmittedRef.current = normalizeEditorMarkdown(editor);
    },
    [editor],
  );

  useEffect(() => {
    if (!editor || editor.isDestroyed || value === undefined) return;

    const previousValue = lastSyncedValueRef.current;
    lastSyncedValueRef.current = value;

    if (value === previousValue) return;

    if (hasUploadingNode(editor)) return;

    const current = normalizeEditorMarkdown(editor);
    const isDirty = lastEmittedRef.current !== null && current !== lastEmittedRef.current;

    if (editor.isFocused && isDirty) return;

    if (isDirty) return;

    applyExternalContent(value);
  }, [value, editor, applyExternalContent]);

  useEffect(() => {
    if (placeholderRef.current === placeholderText) return;
    placeholderRef.current = placeholderText;
    if (!editor || editor.isDestroyed) return;
    editor.view.dispatch(editor.state.tr);
  }, [editor, placeholderText]);

  useImperativeHandle(ref, () => ({
    getMarkdown: () => editor?.getMarkdown() ?? '',
    clearContent: () => {
      editor?.commands.clearContent();
    },
    focus: () => {
      if (editor) editor.commands.focus();
      // Editor not mounted yet — defer the focus to `onCreate`.
      else focusOnReadyRef.current = true;
    },
    focusAtCoords: (coords: { x: number; y: number }) => {
      if (!editor) {
        focusOnReadyRef.current = true;
        return;
      }
      const pos = editor.view.posAtCoords({ left: coords.x, top: coords.y });
      if (pos) editor.commands.focus(pos.pos);
      else editor.commands.focus('end');
    },
    focusAtAnchor: (anchor: TextAnchor) => {
      if (!editor) {
        focusOnReadyRef.current = true;
        return;
      }
      editor.commands.focus(posFromAnchor(editor.state.doc, anchor));
    },
    blur: () => {
      editor?.commands.blur();
    },
    uploadFile: (file: File) => {
      if (!editor || !onUploadFileRef.current) return;
      const endPos = editor.state.doc.content.size;
      uploadAndInsertFile(editor, file, onUploadFileRef.current, endPos);
    },
    hasActiveUploads: () => (editor ? hasUploadingNode(editor) : false),
    insertUploadPlaceholder: (upload) => {
      if (!editor || editor.isDestroyed) return false;
      return insertUploadPlaceholder(editor, upload);
    },
    settleUploadPlaceholder: (uploadId, result) => {
      if (!editor || editor.isDestroyed) return false;
      return settleUploadNode(editor, uploadId, result);
    },
    insertMarkdownAtEnd: (markdown: string) => {
      if (!editor || editor.isDestroyed) return false;
      editor.commands.insertContentAt(editor.state.doc.content.size, markdown, {
        contentType: 'markdown',
      });
      return true;
    },
    flushPendingUpdate: () => {
      if (!debounceRef.current) return null;
      clearTimeout(debounceRef.current);
      debounceRef.current = undefined;
      pendingFlushRef.current = null;
      if (!editor || editor.isDestroyed) return null;
      const md = normalizeEditorMarkdown(editor);
      if (md === lastEmittedRef.current) return null;
      lastEmittedRef.current = md;
      return md;
    },
    adoptContent: (markdown: string) => applyExternalContent(markdown),
  }));

  const wrapperRef = useRef<HTMLDivElement>(null);
  const hoverDisabled = !editor?.state.selection.empty;
  const hover = useLinkHover(wrapperRef, hoverDisabled);

  const handleContainerMouseDown = (event: ReactMouseEvent<HTMLDivElement>) => {
    if (!editor) return;

    const target = event.target as HTMLElement;
    if (target.closest('.ProseMirror')) return;
    if (target.closest("a, button, input, textarea, [role='button'], [data-node-view-wrapper]"))
      return;

    event.preventDefault();
    editor.commands.focus('end');
  };

  if (!editor) return null;

  return (
    <AttachmentDownloadProvider attachments={providerAttachments}>
      <div
        ref={wrapperRef}
        className="relative flex flex-1 min-h-full flex-col"
        onMouseDown={handleContainerMouseDown}
      >
        <EditorContent className="flex flex-1 flex-col" editor={editor} />
        {showBubbleMenu && <EditorBubbleMenu editor={editor} currentIssueId={currentIssueId} />}
        <LinkHoverCard {...hover} />
      </div>
    </AttachmentDownloadProvider>
  );
});

export { ContentEditor, type ContentEditorProps, type ContentEditorRef };
