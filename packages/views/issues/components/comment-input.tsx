'use client';

import { useRef, useState, useCallback, useEffect } from 'react';
import { cn } from '@goosar/ui/lib/utils';
import {
  ContentEditor,
  type ContentEditorRef,
  useFileDropZone,
  FileDropOverlay,
  useLazyEditor,
  useUploadGate,
  useComposerSubmit,
} from '../../editor';
import { FileUploadButton } from '@goosar/ui/components/common/file-upload-button';
import { SubmitButton } from '@goosar/ui/components/common/submit-button';
import { contentReferencesAttachment } from '@goosar/core/types';
import { formatShortcut, useShortcut } from '@goosar/core/shortcuts';
import { useCommentComposerStore, useCommentDraftStore } from '@goosar/core/issues/stores';
import { useT } from '../../i18n';
import { CommentTriggerChips } from './comment-trigger-chips';
import { useCommentTriggerPreview } from '../hooks/use-comment-trigger-preview';
import { useCommentUploads } from './use-comment-uploads';

interface CommentInputProps {
  issueId: string;
  onSubmit: (
    content: string,
    attachmentIds?: string[],
    suppressAgentIds?: string[],
  ) => Promise<boolean>;
}

function CommentInput({ issueId, onSubmit }: CommentInputProps) {
  const { t } = useT('issues');
  const { t: tEditor } = useT('editor');
  const sendShortcut = useShortcut('send');
  const editorRef = useRef<ContentEditorRef>(null);
  const uploadGate = useUploadGate(editorRef);
  const draftKey = `new:${issueId}` as const;
  const [initialDraft] = useState(() => useCommentDraftStore.getState().getDraft(draftKey));
  const [content, setContent] = useState(initialDraft ?? '');
  const [isEmpty, setIsEmpty] = useState(() => !initialDraft?.trim());
  const [suppressedAgentIds, setSuppressedAgentIds] = useState<Set<string>>(() => new Set());
  const triggerPreview = useCommentTriggerPreview({ issueId, content });
  const {
    attachments: pendingAttachments,
    handleUpload,
    gate,
  } = useCommentUploads(draftKey, { issueId }, uploadGate, editorRef);

  const lazy = useLazyEditor({
    initialActive:
      !!initialDraft?.trim() || useCommentDraftStore.getState().getUploads(draftKey).length > 0,
    editorRef,
  });
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: lazy.uploadOrQueue,
  });
  const sticky = useCommentComposerStore((s) => s.sticky);

  const setDraft = useCommentDraftStore((s) => s.setDraft);
  useEffect(() => {
    const flush = () => {
      const md = editorRef.current?.getMarkdown();
      if (md && md.trim().length > 0) setDraft(draftKey, md);
    };
    const onVis = () => {
      if (document.visibilityState === 'hidden') flush();
    };
    document.addEventListener('visibilitychange', onVis);
    window.addEventListener('pagehide', flush);
    return () => {
      document.removeEventListener('visibilitychange', onVis);
      window.removeEventListener('pagehide', flush);
    };
  }, [draftKey, setDraft]);

  useEffect(() => {
    setSuppressedAgentIds(new Set());
  }, [issueId]);

  useEffect(() => {
    const visible = new Set(triggerPreview.agents.map((agent) => agent.id));
    setSuppressedAgentIds((prev) => {
      const next = new Set([...prev].filter((id) => visible.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [triggerPreview.agents]);

  const toggleSuppressedAgent = useCallback((agentId: string) => {
    setSuppressedAgentIds((prev) => {
      const next = new Set(prev);
      if (next.has(agentId)) next.delete(agentId);
      else next.add(agentId);
      return next;
    });
  }, []);

  const mountedRef = useRef(true);
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);
  const submittedEntryRef = useRef<unknown>(null);
  const editorScrubbedRef = useRef(false);

  const { submitting, submit } = useComposerSubmit({
    editorRef,
    uploadGate: gate,
    afterAccepted: () => (editorScrubbedRef.current ? 'blur' : 'none'),
    onSubmit: (content) => {
      editorScrubbedRef.current = false;
      const pending = editorRef.current?.flushPendingUpdate?.();
      if (pending != null) setDraft(draftKey, pending);
      submittedEntryRef.current = useCommentDraftStore.getState().drafts[draftKey];
      const activeIds = pendingAttachments
        .filter((a) => contentReferencesAttachment(content, a))
        .map((a) => a.id);
      const suppressAgentIds = triggerPreview.agents
        .filter((agent) => suppressedAgentIds.has(agent.id))
        .map((agent) => agent.id);
      return onSubmit(
        content,
        activeIds.length > 0 ? activeIds : undefined,
        suppressAgentIds.length > 0 ? suppressAgentIds : undefined,
      );
    },
    onAccepted: () => {
      const lateMd = editorRef.current?.flushPendingUpdate?.();
      if (lateMd != null) setDraft(draftKey, lateMd);
      const store = useCommentDraftStore.getState();
      const live = store.drafts[draftKey];
      const untouched = live === undefined || live === submittedEntryRef.current;
      if (untouched) store.clearDraft(draftKey);
      if (!mountedRef.current || !untouched) return;
      editorRef.current?.clearContent();
      setContent('');
      setIsEmpty(true);
      setSuppressedAgentIds(new Set());
      editorScrubbedRef.current = true;
    },
  });

  return (
    <div
      {...dropZoneProps}
      className="relative flex flex-col rounded-lg bg-card pb-8 ring-1 ring-border"
    >
      {/* Lock the editor while the send is in flight. ContentEditor can't
          toggle Tiptap's `editable` post-mount (see its docstring), so the
          documented way to make it non-interactive is a pointer-events-none +
          dimmed wrapper. */}
      {lazy.active && (
        <div
          className={cn(
            'flex-1 min-h-0 overflow-y-auto px-3 py-2',
            sticky && 'max-h-[40vh]',
            submitting && 'pointer-events-none opacity-60',
            !lazy.ready && 'hidden',
          )}
          aria-busy={submitting || undefined}
        >
          <ContentEditor
            ref={editorRef}
            defaultValue={initialDraft}
            onReady={lazy.onReady}
            placeholder={t(($) => $.comment.leave_comment_placeholder)}
            onUpdate={(md) => {
              setContent(md);
              setIsEmpty(!md.trim());
              setDraft(draftKey, md);
            }}
            onSubmit={submit}
            onUploadFile={handleUpload}
            onUploadingChange={uploadGate.onUploadingChange}
            debounceMs={100}
            currentIssueId={issueId}
            attachments={pendingAttachments}
            enableSlashCommands
            slashCommandMode="command"
          />
        </div>
      )}
      {/* Static shell — visually clones the empty single-line composer.
          Real editor mounts (hidden) on first intent; shell stays visible
          until it's ready so the card never blanks or shifts. */}
      {!lazy.ready && (
        <div
          data-testid="comment-composer-shell"
          role="button"
          tabIndex={0}
          aria-label={t(($) => $.comment.leave_comment_placeholder)}
          className="flex-1 min-h-0 cursor-text px-3 py-2"
          onClick={() => lazy.activate()}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault();
              lazy.activate();
            }
          }}
        >
          {/* rich-text-editor + <p>: the shell line inherits the editor's
              exact type metrics (line-height 1.625 from prose.css), so the
              shell→editor swap doesn't shift layout. */}
          <div className="rich-text-editor text-sm">
            <p className="text-muted-foreground">{t(($) => $.comment.leave_comment_placeholder)}</p>
          </div>
        </div>
      )}
      <div className="absolute bottom-1 left-2 right-28 min-w-0">
        <CommentTriggerChips
          agents={triggerPreview.agents}
          blocked={triggerPreview.blocked}
          draftContent={content}
          suppressedAgentIds={suppressedAgentIds}
          onToggle={toggleSuppressedAgent}
        />
      </div>
      <div className="absolute bottom-1 right-1.5 flex items-center gap-1">
        <FileUploadButton size="sm" multiple onSelect={(file) => lazy.uploadOrQueue([file])} />
        <SubmitButton
          onClick={submit}
          disabled={isEmpty}
          loading={submitting}
          busy={gate.uploading}
          tooltip={
            gate.uploading
              ? tEditor(($) => $.upload.in_progress)
              : sendShortcut
                ? `${t(($) => $.comment.send_tooltip)} · ${formatShortcut(sendShortcut)}`
                : t(($) => $.comment.send_tooltip)
          }
          ariaLabel={
            gate.uploading ? tEditor(($) => $.upload.in_progress) : t(($) => $.comment.send_tooltip)
          }
        />
      </div>
      {isDragOver && <FileDropOverlay />}
    </div>
  );
}

export { CommentInput };
