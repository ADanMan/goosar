'use client';

import { useRef, useState, useCallback, useEffect } from 'react';
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
import { ActorAvatar } from '../../common/actor-avatar';
import { contentReferencesAttachment } from '@goosar/core/types';
import { formatShortcut, useShortcut } from '@goosar/core/shortcuts';
import { useCommentDraftStore, type CommentDraftKey } from '@goosar/core/issues/stores';
import { cn } from '@goosar/ui/lib/utils';
import type { AvatarSize } from '@goosar/ui/lib/avatar-size';
import { useT } from '../../i18n';
import { CommentTriggerChips } from './comment-trigger-chips';
import { useCommentTriggerPreview } from '../hooks/use-comment-trigger-preview';
import { useCommentUploads } from './use-comment-uploads';

interface ReplyInputProps {
  issueId: string;
  parentId: string;
  placeholder?: string;
  avatarType: string;
  avatarId: string;
  onSubmit: (
    content: string,
    attachmentIds?: string[],
    suppressAgentIds?: string[],
  ) => Promise<boolean>;
  size?: 'sm' | 'default';
  draftKey?: CommentDraftKey;
}

function ReplyInput({
  issueId,
  parentId,
  placeholder,
  avatarType,
  avatarId,
  onSubmit,
  size = 'default',
  draftKey,
}: ReplyInputProps) {
  const { t } = useT('issues');
  const { t: tEditor } = useT('editor');
  const sendShortcut = useShortcut('send');
  const placeholderText = placeholder ?? t(($) => $.reply.placeholder);
  const editorRef = useRef<ContentEditorRef>(null);
  const composerRef = useRef<HTMLDivElement>(null);
  const uploadGate = useUploadGate(editorRef);
  const [initialDraft] = useState(() =>
    draftKey ? useCommentDraftStore.getState().getDraft(draftKey) : undefined,
  );
  const [content, setContent] = useState(initialDraft ?? '');
  const setDraft = useCommentDraftStore((s) => s.setDraft);
  const [isEmpty, setIsEmpty] = useState(!initialDraft?.trim());
  const [suppressedAgentIds, setSuppressedAgentIds] = useState<Set<string>>(() => new Set());
  const triggerPreview = useCommentTriggerPreview({ issueId, parentId, content });
  const {
    uploads,
    attachments: pendingAttachments,
    handleUpload,
    removeUpload,
    gate,
  } = useCommentUploads(draftKey, { issueId }, uploadGate, editorRef);

  const lazy = useLazyEditor({
    initialActive:
      !!initialDraft?.trim() ||
      (draftKey ? useCommentDraftStore.getState().getUploads(draftKey).length > 0 : false),
    editorRef,
  });
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: lazy.uploadOrQueue,
  });

  useEffect(() => {
    if (!draftKey) return;
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
  }, [issueId, parentId]);

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
    containerRef: composerRef,
    afterAccepted: () => (editorScrubbedRef.current ? 'refocus' : 'none'),
    onSubmit: (content) => {
      editorScrubbedRef.current = false;
      if (draftKey) {
        const pending = editorRef.current?.flushPendingUpdate?.();
        if (pending != null) setDraft(draftKey, pending);
        submittedEntryRef.current = useCommentDraftStore.getState().drafts[draftKey];
      }
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
      if (draftKey) {
        const lateMd = editorRef.current?.flushPendingUpdate?.();
        if (lateMd != null) setDraft(draftKey, lateMd);
        const store = useCommentDraftStore.getState();
        const live = store.drafts[draftKey];
        const untouched = live === undefined || live === submittedEntryRef.current;
        if (untouched) store.clearDraft(draftKey);
        if (!mountedRef.current || !untouched) return;
      } else {
        if (!mountedRef.current) return;
        uploads.forEach((u) => removeUpload(u.clientUploadId));
      }
      editorRef.current?.clearContent();
      setContent('');
      setIsEmpty(true);
      setSuppressedAgentIds(new Set());
      editorScrubbedRef.current = true;
    },
  });

  const avatarSize: AvatarSize = size === 'sm' ? 'sm' : 'md';

  return (
    <div className="group/editor flex items-start gap-2.5">
      <ActorAvatar
        actorType={avatarType}
        actorId={avatarId}
        size={avatarSize}
        className="mt-0.5 shrink-0"
      />
      <div
        {...dropZoneProps}
        ref={composerRef}
        className={cn('relative min-w-0 flex-1 flex flex-col', !isEmpty && 'pb-9')}
      >
        {/* Lock the editor while the reply is in flight — see CommentInput. */}
        {lazy.active && (
          <div
            className={cn(
              'flex-1 min-h-0 overflow-y-auto',
              submitting && 'pointer-events-none opacity-60',
              !lazy.ready && 'hidden',
            )}
            aria-busy={submitting || undefined}
          >
            <ContentEditor
              ref={editorRef}
              defaultValue={initialDraft}
              onReady={lazy.onReady}
              placeholder={placeholderText}
              onUpdate={(md) => {
                setContent(md);
                setIsEmpty(!md.trim());
                if (draftKey) setDraft(draftKey, md);
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
        {/* Static shell — clones the empty single-line reply box (see
            CommentInput for the pattern). */}
        {!lazy.ready && (
          <div
            data-testid="reply-composer-shell"
            role="button"
            tabIndex={0}
            aria-label={placeholderText}
            className="flex-1 min-h-0 cursor-text rich-text-editor text-sm"
            onClick={() => lazy.activate()}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                lazy.activate();
              }
            }}
          >
            {/* <p> under rich-text-editor: same type metrics as the real
                editor's empty paragraph — no height jump on swap. */}
            <p className="text-muted-foreground">{placeholderText}</p>
          </div>
        )}
        <div className="absolute bottom-0 left-0 right-24 min-w-0">
          <CommentTriggerChips
            agents={triggerPreview.agents}
            blocked={triggerPreview.blocked}
            draftContent={content}
            suppressedAgentIds={suppressedAgentIds}
            onToggle={toggleSuppressedAgent}
          />
        </div>
        <div className="absolute bottom-0 right-0 flex items-center gap-1">
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
              gate.uploading
                ? tEditor(($) => $.upload.in_progress)
                : t(($) => $.comment.send_tooltip)
            }
          />
        </div>
        {isDragOver && <FileDropOverlay />}
      </div>
    </div>
  );
}

export { ReplyInput, type ReplyInputProps };
