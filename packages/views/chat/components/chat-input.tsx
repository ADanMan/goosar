'use client';

import type { ReactNode } from 'react';
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { TriangleAlert } from 'lucide-react';
import { cn } from '@goosar/ui/lib/utils';
import {
  ContentEditor,
  type ContentEditorRef,
  useFileDropZone,
  FileDropOverlay,
  useUploadGate,
  useComposerSubmit,
} from '../../editor';
import { PASTE_AS_FILE_THRESHOLD } from '../../editor/paste-as-file';
import {
  useCoordinatedUploads,
  type UploadDraftBinding,
} from '../../editor/use-coordinated-uploads';
import { SubmitButton } from '@goosar/ui/components/common/submit-button';
import { ChatAddMenu } from './chat-add-menu';
import { useChatStore, DRAFT_NEW_SESSION } from '@goosar/core/chat';
import { attachmentToDraftUpload, type DraftUpload } from '@goosar/core/drafts';
import { createLogger } from '@goosar/core/logger';
import { formatShortcut, useShortcut } from '@goosar/core/shortcuts';
import type { MentionItem } from '../../editor/extensions/mention-suggestion';
import type { Attachment, Project } from '@goosar/core/types';
import { ProjectPicker } from '../../projects/components/project-picker';
import { useT } from '../../i18n';

const logger = createLogger('chat.ui');
const EMPTY_UPLOADS: DraftUpload[] = [];
const CHAT_COMPOSER_EDITOR_KEY = 'chat-composer';

function attachmentReferenceUrls(attachment: Attachment): string[] {
  const withUploadFields = attachment as Attachment & {
    markdownLink?: string;
    link?: string;
  };
  return [
    withUploadFields.markdownLink,
    attachment.markdown_url,
    attachment.download_url,
    attachment.url,
    withUploadFields.link,
    attachment.id ? `/api/attachments/${attachment.id}/download` : '',
  ].filter((url): url is string => !!url);
}

function isAttachmentReferenced(content: string, attachment: Attachment): boolean {
  return attachmentReferenceUrls(attachment).some((url) => content.includes(url));
}

interface ChatInputProps {
  onSend: (
    content: string,
    attachmentIds: string[] | undefined,
    commitInput: (options?: { extraDraftKeys?: string[]; clearEditor?: boolean }) => void,
    draftAttachments: Attachment[],
  ) => void | boolean | Promise<void | boolean>;
  restoreDraftRequest?: {
    id: string;
    content: string;
    attachments?: Attachment[];
    sessionId?: string;
  } | null;
  onRestoreDraftApplied?: () => void;
  uploadEnabled?: boolean;
  onStop?: () => void;
  isRunning?: boolean;
  disabled?: boolean;
  noAgent?: boolean;
  agentArchived?: boolean;
  agentName?: string;
  leftAdornment?: ReactNode;
  contextItems?: MentionItem[];
  projects?: Project[];
  projectId?: string | null;
  onProjectChange?: (projectId: string | null) => void;
  isProjectUpdating?: boolean;
  projectContextUnsupported?: boolean;
  focusRequest?: number;
  draftKeyOverride?: string;
  editorKeyOverride?: string;
}

export function ChatInput({
  onSend,
  restoreDraftRequest,
  onRestoreDraftApplied,
  uploadEnabled: uploadAllowed,
  onStop,
  isRunning,
  disabled,
  noAgent,
  agentArchived,
  agentName,
  leftAdornment,
  contextItems,
  projects = [],
  projectId,
  onProjectChange,
  isProjectUpdating,
  projectContextUnsupported,
  focusRequest,
  draftKeyOverride,
  editorKeyOverride,
}: ChatInputProps) {
  const { t } = useT('chat');
  const { t: tEditor } = useT('editor');
  const sendShortcut = useShortcut('send');
  const editorRef = useRef<ContentEditorRef>(null);
  const composerRef = useRef<HTMLDivElement>(null);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const draftKey = draftKeyOverride ?? activeSessionId ?? DRAFT_NEW_SESSION;
  const inputDraft = useChatStore((s) => s.inputDrafts[draftKey] ?? '');
  const storeUploads = useChatStore((s) => s.inputDraftAttachments[draftKey] ?? EMPTY_UPLOADS);
  const setInputDraft = useChatStore((s) => s.setInputDraft);
  const setInputDraftAttachments = useChatStore((s) => s.setInputDraftAttachments);
  const clearInputDraft = useChatStore((s) => s.clearInputDraft);
  const [isEmpty, setIsEmpty] = useState(!inputDraft.trim());
  const hasNothingToSend = isEmpty && !inputDraft.trim();
  const appliedRestoreIdRef = useRef<string | null>(null);
  const editorKey = editorKeyOverride ?? CHAT_COMPOSER_EDITOR_KEY;

  const editorDraftKeyRef = useRef(draftKey);

  const commitDraft = useCallback(
    (key: string, markdown: string) => {
      setInputDraft(key, markdown);
      const uploads = useChatStore.getState().inputDraftAttachments[key] ?? EMPTY_UPLOADS;
      if (uploads.length === 0) return;
      const referenced = uploads.filter(
        (u) => u.status !== 'uploaded' || isAttachmentReferenced(markdown, u.attachment),
      );
      if (referenced.length !== uploads.length) {
        setInputDraftAttachments(key, referenced);
      }
    },
    [setInputDraft, setInputDraftAttachments],
  );
  const uploadGate = useUploadGate(editorRef);

  const [loadedDraftKey, setLoadedDraftKey] = useState(draftKey);

  const makeUploadBinding = useCallback(
    (key: string): UploadDraftBinding => ({
      registryKey: `chat:${key}`,
      getUploads: () => useChatStore.getState().inputDraftAttachments[key] ?? EMPTY_UPLOADS,
      addUpload: (u) => useChatStore.getState().addInputDraftUpload(key, u),
      settleUpload: (id, att) => useChatStore.getState().settleInputDraftUpload(key, id, att),
      failUpload: (id, err) => useChatStore.getState().failInputDraftUpload(key, id, err),
      removeUpload: (id) => useChatStore.getState().removeInputDraftUpload(key, id),
      getBody: () => useChatStore.getState().inputDrafts[key] ?? '',
      appendToBody: (md) => useChatStore.getState().appendToInputDraft(key, md),
    }),
    [],
  );
  const uploadBinding = useMemo(() => makeUploadBinding(draftKey), [makeUploadBinding, draftKey]);
  const {
    uploads: draftUploads,
    attachments: draftAttachments,
    handleUpload,
    gate,
  } = useCoordinatedUploads(uploadBinding, storeUploads, {}, uploadGate, editorRef, {
    resolveUploadTarget: () => makeUploadBinding(editorDraftKeyRef.current),
    liveRegistryKey: `chat:${loadedDraftKey}`,
  });

  const deferredAdoptRef = useRef(false);
  useLayoutEffect(() => {
    const loadedKey = editorDraftKeyRef.current;
    if (loadedKey === draftKey) return;
    if (editorRef.current?.hasActiveUploads() === true) {
      deferredAdoptRef.current = true;
      logger.debug('input.draft switch deferred by in-flight upload', {
        from: loadedKey,
        to: draftKey,
      });
      return;
    }
    const pending = editorRef.current?.flushPendingUpdate() ?? null;
    if (pending !== null) {
      logger.debug('input.draft flush on key change', { from: loadedKey, to: draftKey });
      commitDraft(loadedKey, pending);
    }
    editorDraftKeyRef.current = draftKey;
    setLoadedDraftKey(draftKey);
    if (!deferredAdoptRef.current) return;
    deferredAdoptRef.current = false;
    const incoming = useChatStore.getState().inputDrafts[draftKey] ?? '';
    logger.debug('input.draft adopting after upload settled', { key: draftKey });
    editorRef.current?.adoptContent(incoming);
    setIsEmpty(!incoming.trim());
  }, [draftKey, uploadGate.uploading, commitDraft]);

  useEffect(() => {
    if (!focusRequest) return;
    editorRef.current?.focus();
  }, [focusRequest]);

  useEffect(() => {
    if (!restoreDraftRequest) {
      appliedRestoreIdRef.current = null;
      return;
    }
    if (appliedRestoreIdRef.current === restoreDraftRequest.id) return;
    if (restoreDraftRequest.sessionId && restoreDraftRequest.sessionId !== draftKey) {
      return;
    }
    if (inputDraft.trim() || draftUploads.length > 0) {
      logger.debug('input.restore waiting: draft has content', {
        draftKey,
        restoreId: restoreDraftRequest.id,
      });
      return;
    }
    appliedRestoreIdRef.current = restoreDraftRequest.id;
    setInputDraft(draftKey, restoreDraftRequest.content);
    setInputDraftAttachments(
      draftKey,
      (restoreDraftRequest.attachments ?? []).map(attachmentToDraftUpload),
    );
    setIsEmpty(!restoreDraftRequest.content.trim());
    onRestoreDraftApplied?.();
  }, [
    draftKey,
    inputDraft,
    draftUploads,
    onRestoreDraftApplied,
    restoreDraftRequest,
    setInputDraft,
    setInputDraftAttachments,
  ]);

  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
  });

  const editorScrubbedRef = useRef(false);

  const { submitting, submit } = useComposerSubmit({
    editorRef,
    uploadGate: gate,
    containerRef: composerRef,
    afterAccepted: () => (editorScrubbedRef.current ? 'refocus' : 'none'),
    onSubmit: async (content: string): Promise<boolean> => {
      editorScrubbedRef.current = false;
      if (isRunning || disabled || noAgent) {
        logger.debug('input.send skipped', { isRunning, disabled, noAgent });
        return false;
      }
      if (editorDraftKeyRef.current !== draftKey) {
        logger.debug('input.send skipped: composer still holds another draft', {
          loaded: editorDraftKeyRef.current,
          selected: draftKey,
        });
        return false;
      }
      const activeIds: string[] = [];
      for (const attachment of draftAttachments) {
        if (isAttachmentReferenced(content, attachment)) activeIds.push(attachment.id);
      }
      const uniqueActiveIds = Array.from(new Set(activeIds));
      const keyAtSend = draftKey;
      const pendingFlush = editorRef.current?.flushPendingUpdate?.();
      if (pendingFlush != null) commitDraft(editorDraftKeyRef.current, pendingFlush);
      const draftValueAtSend = useChatStore.getState().inputDrafts[keyAtSend];
      let committed = false;
      const commitInput = (options?: { extraDraftKeys?: string[]; clearEditor?: boolean }) => {
        if (committed) return;
        committed = true;
        const lateMd = editorRef.current?.flushPendingUpdate?.();
        if (lateMd != null) commitDraft(editorDraftKeyRef.current, lateMd);
        const liveDraft = useChatStore.getState().inputDrafts[keyAtSend];
        const untouched = liveDraft === undefined || liveDraft === draftValueAtSend;
        if (options?.clearEditor !== false && untouched) {
          editorRef.current?.clearContent();
          editorScrubbedRef.current = true;
          setIsEmpty(true);
        }
        if (untouched) clearInputDraft(keyAtSend);
        for (const key of options?.extraDraftKeys ?? []) {
          if (key !== keyAtSend) clearInputDraft(key);
        }
      };
      logger.info('input.send', {
        contentLength: content.length,
        draftKey: keyAtSend,
        attachmentCount: uniqueActiveIds.length,
      });
      const accepted = await onSend(
        content,
        uniqueActiveIds.length > 0 ? uniqueActiveIds : undefined,
        commitInput,
        draftAttachments.filter((attachment) => uniqueActiveIds.includes(attachment.id)),
      );
      if (accepted === false) return false;
      if (!committed) commitInput();
      return true;
    },
  });

  const placeholder = noAgent
    ? t(($) => $.input.placeholder_no_agent)
    : disabled
      ? agentArchived
        ? t(($) => $.input.placeholder_archived_agent)
        : t(($) => $.input.placeholder_archived)
      : agentName
        ? t(($) => $.input.placeholder_named, { name: agentName })
        : t(($) => $.input.placeholder_default);

  const uploadEnabled = !!uploadAllowed && !disabled && !noAgent;
  const projectSelectionEnabled =
    !!onProjectChange && !disabled && !noAgent && !submitting && !isProjectUpdating;
  const selectedProject = projects.find((project) => project.id === projectId);

  return (
    <div
      ref={composerRef}
      className={cn(
        'flex max-h-[50%] min-h-0 flex-col px-5 pb-3 pt-0',
        noAgent && 'cursor-not-allowed',
      )}
    >
      <div
        {...(uploadEnabled ? dropZoneProps : {})}
        className={cn(
          'relative mx-auto flex min-h-16 max-h-96 w-full max-w-4xl flex-col rounded-lg border border-surface-border bg-surface pb-9 transition-[border-color,box-shadow] focus-within:border-brand focus-within:ring-2 focus-within:ring-ring/20',
          noAgent && 'pointer-events-none opacity-60',
        )}
        aria-disabled={noAgent || undefined}
      >
        {selectedProject && (
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1 px-3 pt-2">
            <div
              className={cn(
                'inline-flex max-w-full',
                !projectSelectionEnabled && 'pointer-events-none opacity-60',
              )}
            >
              <ProjectPicker
                projectId={selectedProject.id}
                onUpdate={(updates) => onProjectChange?.(updates.project_id ?? null)}
                disabled={!projectSelectionEnabled}
                triggerRender={
                  <button
                    type="button"
                    disabled={!projectSelectionEnabled}
                    aria-label={t(($) => $.input.change_project_context)}
                    title={t(($) => $.input.change_project_context)}
                    className="flex h-6 max-w-56 items-center gap-1.5 rounded-full border border-surface-border bg-surface-raised px-2 pr-7 text-xs font-medium text-foreground transition-colors hover:bg-accent/60"
                  />
                }
              />
            </div>
            {projectContextUnsupported && (
              <span className="inline-flex min-w-0 items-center gap-1 text-xs text-warning">
                <TriangleAlert className="size-3 shrink-0" />
                {t(($) => $.input.project_context_unsupported)}
              </span>
            )}
          </div>
        )}
        <div className="flex-1 min-h-0 overflow-y-auto px-3 py-2">
          <ContentEditor
            key={editorKey}
            ref={editorRef}
            value={inputDraft}
            placeholder={placeholder}
            onUpdate={(md) => {
              setIsEmpty(!md.trim());
              commitDraft(editorDraftKeyRef.current, md);
            }}
            onSubmit={submit}
            onUploadFile={uploadEnabled ? handleUpload : undefined}
            pasteAsFileThreshold={PASTE_AS_FILE_THRESHOLD}
            onUploadingChange={uploadGate.onUploadingChange}
            attachments={draftAttachments}
            debounceMs={100}
            mentionMode={contextItems ? 'context' : 'default'}
            mentionContextItems={contextItems}
            enableSlashCommands
            showBubbleMenu
          />
        </div>
        {(uploadEnabled || projectSelectionEnabled || leftAdornment) && (
          <div className="absolute bottom-1.5 left-1.5 flex items-center gap-1">
            {(uploadEnabled || projectSelectionEnabled) && (
              <ChatAddMenu
                onSelectFile={
                  uploadEnabled ? (file) => editorRef.current?.uploadFile(file) : undefined
                }
                projects={projects}
                projectId={projectId}
                onSelectProject={projectSelectionEnabled ? onProjectChange : undefined}
                projectContextUnsupported={projectContextUnsupported}
              />
            )}
            {leftAdornment}
          </div>
        )}
        <div className="absolute bottom-1 right-1.5 flex items-center gap-1">
          <SubmitButton
            onClick={submit}
            disabled={hasNothingToSend || submitting || !!disabled || !!noAgent}
            loading={submitting}
            busy={gate.uploading}
            running={isRunning}
            onStop={onStop}
            tooltip={
              gate.uploading
                ? tEditor(($) => $.upload.in_progress)
                : sendShortcut
                  ? `${t(($) => $.input.send_tooltip)} · ${formatShortcut(sendShortcut)}`
                  : t(($) => $.input.send_tooltip)
            }
            ariaLabel={
              gate.uploading ? tEditor(($) => $.upload.in_progress) : t(($) => $.input.send_tooltip)
            }
            stopTooltip={t(($) => $.input.stop_tooltip)}
            stopAriaLabel={t(($) => $.input.stop_tooltip)}
          />
        </div>
        {uploadEnabled && isDragOver && <FileDropOverlay />}
      </div>
    </div>
  );
}
