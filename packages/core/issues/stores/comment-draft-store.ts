import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from '../../platform/workspace-storage';
import { defaultStorage } from '../../platform/storage';
import { registerDraftCleanup } from '../../drafts/cleanup-registry';
import {
  type DraftUpload,
  type PendingDraftUpload,
  attachmentToDraftUpload,
  normalizeStoredUploads,
  uploadedAttachments,
} from '../../drafts/draft-upload';
import type { Attachment } from '../../types';

export type CommentDraftKey =
  | `new:${string}` 
  | `reply:${string}:${string}` 
  | `edit:${string}:${string}`; 

interface CommentDraft {
  content: string;
  attachments: DraftUpload[];
  updatedAt: number;
}

interface CommentDraftStore {
  drafts: Record<string, CommentDraft>;
  getDraft: (key: CommentDraftKey) => string | undefined;
  getAttachments: (key: CommentDraftKey) => Attachment[];
  getUploads: (key: CommentDraftKey) => DraftUpload[];
  setDraft: (key: CommentDraftKey, content: string) => void;
  appendToDraftContent: (key: CommentDraftKey, markdown: string) => void;
  setAttachments: (key: CommentDraftKey, attachments: Attachment[]) => void;
  addUpload: (key: CommentDraftKey, upload: DraftUpload) => void;
  settleUpload: (key: CommentDraftKey, clientUploadId: string, attachment: Attachment) => void;
  failUpload: (key: CommentDraftKey, clientUploadId: string, error?: string) => void;
  removeUpload: (key: CommentDraftKey, clientUploadId: string) => void;
  clearDraft: (key: CommentDraftKey) => void;
}

const TTL_MS = 30 * 24 * 60 * 60 * 1000;

const EMPTY_UPLOADS: DraftUpload[] = [];
const EMPTY_ATTACHMENTS: Attachment[] = [];

const derivedCache = new WeakMap<DraftUpload[], Attachment[]>();
function deriveUploaded(uploads: DraftUpload[]): Attachment[] {
  const cached = derivedCache.get(uploads);
  if (cached) return cached;
  const derived = uploadedAttachments(uploads);
  const result = derived.length === 0 ? EMPTY_ATTACHMENTS : derived;
  derivedCache.set(uploads, result);
  return result;
}

function isMeaningful(content: string, uploads: DraftUpload[]): boolean {
  return content.trim().length > 0 || uploads.length > 0;
}

function writeDraft(
  drafts: Record<string, CommentDraft>,
  key: string,
  content: string,
  uploads: DraftUpload[],
): Record<string, CommentDraft> {
  if (!isMeaningful(content, uploads)) {
    if (!(key in drafts)) return drafts;
    const next = { ...drafts };
    delete next[key];
    return next;
  }
  const existing = drafts[key];
  if (existing && existing.content === content && existing.attachments === uploads) {
    return drafts;
  }
  return { ...drafts, [key]: { content, attachments: uploads, updatedAt: Date.now() } };
}

function uploadsOf(drafts: Record<string, CommentDraft>, key: string): DraftUpload[] {
  return drafts[key]?.attachments ?? EMPTY_UPLOADS;
}

function pruneStaleDrafts(drafts: Record<string, CommentDraft>): Record<string, CommentDraft> {
  const cutoff = Date.now() - TTL_MS;
  const out: Record<string, CommentDraft> = {};
  for (const [k, v] of Object.entries(drafts)) {
    const uploads = normalizeStoredUploads(v.attachments);
    if (v.updatedAt >= cutoff && isMeaningful(v.content, uploads)) {
      out[k] = { ...v, attachments: uploads };
    }
  }
  return out;
}

export const useCommentDraftStore = create<CommentDraftStore>()(
  persist(
    (set, get) => ({
      drafts: {},
      getDraft: (key) => get().drafts[key]?.content,
      getAttachments: (key) => deriveUploaded(uploadsOf(get().drafts, key)),
      getUploads: (key) => uploadsOf(get().drafts, key),
      setDraft: (key, content) =>
        set((s) => ({
          drafts: writeDraft(s.drafts, key, content, uploadsOf(s.drafts, key)),
        })),
      appendToDraftContent: (key, markdown) =>
        set((s) => {
          const existing = s.drafts[key]?.content ?? '';
          const next = existing.trim()
            ? `${existing.replace(/\s+$/, '')}\n\n${markdown}`
            : markdown;
          return {
            drafts: writeDraft(s.drafts, key, next, uploadsOf(s.drafts, key)),
          };
        }),
      setAttachments: (key, attachments) =>
        set((s) => ({
          drafts: writeDraft(
            s.drafts,
            key,
            s.drafts[key]?.content ?? '',
            attachments.map(attachmentToDraftUpload),
          ),
        })),
      addUpload: (key, upload) =>
        set((s) => {
          const current = uploadsOf(s.drafts, key);
          if (current.some((u) => u.clientUploadId === upload.clientUploadId)) return s;
          return {
            drafts: writeDraft(s.drafts, key, s.drafts[key]?.content ?? '', [...current, upload]),
          };
        }),
      settleUpload: (key, clientUploadId, attachment) =>
        set((s) => {
          const current = uploadsOf(s.drafts, key);
          if (!current.some((u) => u.clientUploadId === clientUploadId)) return s;
          const next = current.map((u) =>
            u.clientUploadId === clientUploadId
              ? { ...attachmentToDraftUpload(attachment), clientUploadId }
              : u,
          );
          return {
            drafts: writeDraft(s.drafts, key, s.drafts[key]?.content ?? '', next),
          };
        }),
      failUpload: (key, clientUploadId, error) =>
        set((s) => {
          const current = uploadsOf(s.drafts, key);
          const target = current.find((u) => u.clientUploadId === clientUploadId);
          if (!target) return s;
          const failed: PendingDraftUpload = {
            clientUploadId,
            status: 'failed',
            filename: target.filename,
            size: target.size,
            contentType: target.contentType,
            error,
          };
          const next = current.map((u) => (u.clientUploadId === clientUploadId ? failed : u));
          return {
            drafts: writeDraft(s.drafts, key, s.drafts[key]?.content ?? '', next),
          };
        }),
      removeUpload: (key, clientUploadId) =>
        set((s) => {
          const current = uploadsOf(s.drafts, key);
          if (!current.some((u) => u.clientUploadId === clientUploadId)) return s;
          const next = current.filter((u) => u.clientUploadId !== clientUploadId);
          return {
            drafts: writeDraft(s.drafts, key, s.drafts[key]?.content ?? '', next),
          };
        }),
      clearDraft: (key) =>
        set((s) => {
          if (!(key in s.drafts)) return s;
          const next = { ...s.drafts };
          delete next[key];
          return { drafts: next };
        }),
    }),
    {
      name: 'goosar_comment_drafts',
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      onRehydrateStorage: () => (state) => {
        if (state) {
          state.drafts = pruneStaleDrafts(state.drafts);
        }
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useCommentDraftStore.persist.rehydrate());

registerDraftCleanup({
  storageKey: 'goosar_comment_drafts',
  workspaceScoped: true,
  resetInMemory: () => useCommentDraftStore.setState({ drafts: {} }),
});
