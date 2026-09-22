import type { Attachment } from '../types';

export type UploadStatus = 'uploading' | 'uploaded' | 'failed' | 'interrupted';

interface DraftUploadBase {
  clientUploadId: string;
  filename: string;
  size: number;
  contentType?: string;
}

export interface PendingDraftUpload extends DraftUploadBase {
  status: 'uploading' | 'failed' | 'interrupted';
  error?: string;
}

export interface UploadedDraftUpload extends DraftUploadBase {
  status: 'uploaded';
  attachment: Attachment;
}

export type DraftUpload = PendingDraftUpload | UploadedDraftUpload;

export function isUploaded(u: DraftUpload): u is UploadedDraftUpload {
  return u.status === 'uploaded';
}

export function uploadedAttachments(uploads: readonly DraftUpload[]): Attachment[] {
  const out: Attachment[] = [];
  for (const u of uploads) {
    if (u.status === 'uploaded') out.push(u.attachment);
  }
  return out;
}

export function hasUploadingDraft(uploads: readonly DraftUpload[]): boolean {
  return uploads.some((u) => u.status === 'uploading');
}

export function attachmentToDraftUpload(attachment: Attachment): UploadedDraftUpload {
  return {
    clientUploadId: attachment.id || attachment.url,
    status: 'uploaded',
    filename: attachment.filename,
    size: attachment.size_bytes,
    contentType: attachment.content_type || undefined,
    attachment: { ...attachment, download_url: '' },
  };
}

function looksLikeAttachment(value: unknown): value is Attachment {
  return (
    typeof value === 'object' &&
    value !== null &&
    typeof (value as { id?: unknown }).id === 'string' &&
    typeof (value as { filename?: unknown }).filename === 'string' &&
    typeof (value as { url?: unknown }).url === 'string'
  );
}

function isDraftUploadShape(value: unknown): value is DraftUpload {
  return (
    typeof value === 'object' &&
    value !== null &&
    typeof (value as { clientUploadId?: unknown }).clientUploadId === 'string' &&
    typeof (value as { status?: unknown }).status === 'string'
  );
}

export function normalizeStoredUploads(raw: unknown): DraftUpload[] {
  if (!Array.isArray(raw)) return [];
  const out: DraftUpload[] = [];
  for (const item of raw) {
    if (isDraftUploadShape(item)) {
      if (item.status === 'uploading') {
        continue;
      } else if (item.status === 'uploaded') {
        if (looksLikeAttachment((item as UploadedDraftUpload).attachment)) {
          out.push(item);
        }
      } else {
        out.push(item);
      }
    } else if (looksLikeAttachment(item)) {
      out.push(attachmentToDraftUpload(item));
    }
  }
  return out;
}
