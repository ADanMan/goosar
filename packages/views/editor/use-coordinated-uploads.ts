'use client';

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type RefObject,
} from 'react';
import { toast } from 'sonner';
import { api } from '@goosar/core/api';
import {
  startUpload,
  abortUpload,
  hasUploadingDraft,
  attachmentToDraftUpload,
  type DraftUpload,
} from '@goosar/core/drafts';
import { createSafeId } from '@goosar/core/utils';
import { contentReferencesAttachment, type Attachment } from '@goosar/core/types';
import {
  toUploadResult,
  type UploadContext,
  type UploadResult,
} from '@goosar/core/hooks/use-file-upload';
import { MAX_FILE_SIZE } from '@goosar/core/constants/upload';
import { useT } from '../i18n';
import type { UploadGate } from './use-upload-gate';
import type { ContentEditorRef } from './content-editor';
import { pastedTextSource } from './extensions/file-upload';

const EMPTY_ATTACHMENTS: Attachment[] = [];

export interface UploadDraftBinding {
  registryKey: string;
  getUploads: () => DraftUpload[];
  addUpload: (upload: DraftUpload) => void;
  settleUpload: (clientUploadId: string, attachment: Attachment) => void;
  failUpload: (clientUploadId: string, error?: string) => void;
  removeUpload: (clientUploadId: string) => void;
  getBody: () => string;
  appendToBody: (markdown: string) => void;
}

const liveEditors = new Map<string, RefObject<ContentEditorRef | null>>();

export function __liveEditorRegistryKeysForTest(): string[] {
  return [...liveEditors.keys()];
}

export function attachmentMarkdown(att: Attachment): string {
  const link = toUploadResult(att).markdownLink;
  return (att.content_type ?? '').startsWith('image/')
    ? `![${att.filename}](${link})`
    : `[${att.filename}](${link})`;
}

const DELIVER_RETRY_MS = 50;
const DELIVER_MAX_TRIES = 100; 

function deliverFinishedUpload(
  binding: UploadDraftBinding,
  clientUploadId: string,
  attachment: Attachment,
  tries = 0,
): void {
  if (!binding.getUploads().some((u) => u.clientUploadId === clientUploadId)) return;
  if (contentReferencesAttachment(binding.getBody(), attachment)) return;

  const md = attachmentMarkdown(attachment);
  const live = liveEditors.get(binding.registryKey);
  if (live?.current?.settleUploadPlaceholder(clientUploadId, toUploadResult(attachment)) === true) {
    binding.appendToBody(md);
    return;
  }
  if (live?.current?.insertMarkdownAtEnd(md) === true) {
    binding.appendToBody(md);
    return;
  }
  if (!live) {
    binding.appendToBody(md);
    return;
  }
  if (tries >= DELIVER_MAX_TRIES) {
    binding.appendToBody(md);
    return;
  }
  setTimeout(
    () => deliverFinishedUpload(binding, clientUploadId, attachment, tries + 1),
    DELIVER_RETRY_MS,
  );
}

function deliverPastedTextBack(
  binding: UploadDraftBinding | undefined,
  editorRef: RefObject<ContentEditorRef | null>,
  text: string,
): void {
  if (!binding) {
    editorRef.current?.insertMarkdownAtEnd(text);
    return;
  }
  liveEditors.get(binding.registryKey)?.current?.insertMarkdownAtEnd(text);
  binding.appendToBody(text);
}

export interface CoordinatedUploads {
  uploads: DraftUpload[];
  attachments: Attachment[];
  handleUpload: (file: File) => Promise<UploadResult | null>;
  removeUpload: (clientUploadId: string) => void;
  gate: UploadGate;
}

export function useCoordinatedUploads(
  binding: UploadDraftBinding | undefined,
  boundUploads: DraftUpload[],
  ctx: UploadContext,
  editorGate: UploadGate,
  editorRef: RefObject<ContentEditorRef | null>,
  opts?: {
    resolveUploadTarget?: () => UploadDraftBinding;
    liveRegistryKey?: string;
  },
): CoordinatedUploads {
  const { t } = useT('editor');
  const [localUploads, setLocalUploads] = useState<DraftUpload[]>([]);
  const resolveUploadTargetRef = useRef(opts?.resolveUploadTarget);
  resolveUploadTargetRef.current = opts?.resolveUploadTarget;

  const mountedRef = useRef(true);
  useLayoutEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const registryKey = binding ? (opts?.liveRegistryKey ?? binding.registryKey) : undefined;
  useLayoutEffect(() => {
    if (!registryKey) return;
    liveEditors.set(registryKey, editorRef);
    return () => {
      if (liveEditors.get(registryKey) === editorRef) liveEditors.delete(registryKey);
    };
  }, [registryKey, editorRef]);

  const uploads = binding ? boundUploads : localUploads;
  const attachments = useMemo(() => {
    const done: Attachment[] = [];
    for (const u of uploads) {
      if (u.status === 'uploaded') done.push(u.attachment);
    }
    return done.length === 0 ? EMPTY_ATTACHMENTS : done;
  }, [uploads]);

  const rebuiltUploadIdsRef = useRef<Set<string>>(new Set());
  const editorHoldsThisTarget = !binding || registryKey === binding.registryKey;
  useEffect(() => {
    if (!editorHoldsThisTarget) return;
    const pending = uploads.filter(
      (u) => u.status === 'uploading' && !rebuiltUploadIdsRef.current.has(u.clientUploadId),
    );
    if (pending.length === 0) return;
    let cancelled = false;
    let tries = 0;
    const attempt = () => {
      if (cancelled) return;
      const missing = pending.filter((u) => {
        const landed = editorRef.current?.insertUploadPlaceholder({
          uploadId: u.clientUploadId,
          filename: u.filename,
          size: u.size,
        });
        if (landed === true) rebuiltUploadIdsRef.current.add(u.clientUploadId);
        return landed !== true;
      });
      if (missing.length === 0) return;
      if (++tries >= DELIVER_MAX_TRIES) return;
      setTimeout(attempt, DELIVER_RETRY_MS);
    };
    attempt();
    return () => {
      cancelled = true;
    };
  }, [uploads, editorRef, editorHoldsThisTarget]);

  const issueId = ctx.issueId;
  const commentId = ctx.commentId;
  const chatSessionId = ctx.chatSessionId;

  const handleUpload = useCallback(
    (file: File, uploadId?: string): Promise<UploadResult | null> => {
      const clientUploadId = uploadId ?? createSafeId();
      if (uploadId) rebuiltUploadIdsRef.current.add(uploadId);
      const target = binding ? (resolveUploadTargetRef.current?.() ?? binding) : undefined;
      const placeholder: DraftUpload = {
        clientUploadId,
        status: 'uploading',
        filename: file.name,
        size: file.size,
        contentType: file.type || undefined,
      };

      const pastedText = pastedTextSource(file);

      if (file.size > MAX_FILE_SIZE) {
        const reason = 'File exceeds 100 MB limit';
        if (pastedText !== undefined) {
          deliverPastedTextBack(target, editorRef, pastedText);
        }
        toast.error(t(($) => $.upload.failed, { filename: file.name, reason }));
        return Promise.resolve(null);
      }

      if (target) {
        target.addUpload(placeholder);
      } else {
        setLocalUploads((prev) => [...prev, placeholder]);
      }

      return new Promise<UploadResult | null>((resolve) => {
        startUpload({
          clientUploadId,
          file,
          api,
          ctx: { issueId, commentId, chatSessionId },
          onSettled: (outcome) => {
            if (outcome.status === 'uploaded') {
              if (target) {
                if (target.getUploads().some((u) => u.clientUploadId === clientUploadId)) {
                  target.settleUpload(clientUploadId, outcome.attachment);
                  if (!mountedRef.current) {
                    deliverFinishedUpload(target, clientUploadId, outcome.attachment);
                  }
                }
              } else {
                setLocalUploads((prev) =>
                  prev.map((u) =>
                    u.clientUploadId === clientUploadId
                      ? { ...attachmentToDraftUpload(outcome.attachment), clientUploadId }
                      : u,
                  ),
                );
              }
              resolve(toUploadResult(outcome.attachment));
            } else {
              const reason = outcome.error.message;
              if (target) {
                if (target.getUploads().some((u) => u.clientUploadId === clientUploadId)) {
                  target.removeUpload(clientUploadId);
                  if (pastedText !== undefined) {
                    deliverPastedTextBack(target, editorRef, pastedText);
                  }
                }
              } else {
                setLocalUploads((prev) => prev.filter((u) => u.clientUploadId !== clientUploadId));
                if (pastedText !== undefined) {
                  deliverPastedTextBack(undefined, editorRef, pastedText);
                }
              }
              toast.error(t(($) => $.upload.failed, { filename: file.name, reason }));
              resolve(null);
            }
          },
        });
      });
    },
    [binding, editorRef, issueId, commentId, chatSessionId, t],
  );

  const removeUpload = useCallback(
    (clientUploadId: string) => {
      const tracked = binding ? binding.getUploads() : localUploadsRef.current;
      if (tracked.some((u) => u.clientUploadId === clientUploadId && u.status === 'uploading')) {
        abortUpload(clientUploadId);
      }
      if (binding) binding.removeUpload(clientUploadId);
      else setLocalUploads((prev) => prev.filter((u) => u.clientUploadId !== clientUploadId));
    },
    [binding],
  );

  const localUploadsRef = useRef(localUploads);
  localUploadsRef.current = localUploads;

  const gate: UploadGate = {
    uploading: editorGate.uploading || hasUploadingDraft(uploads),
    onUploadingChange: editorGate.onUploadingChange,
    isBlocked: () =>
      editorGate.isBlocked() ||
      hasUploadingDraft(binding ? binding.getUploads() : localUploadsRef.current),
  };

  return { uploads, attachments, handleUpload, removeUpload, gate };
}
